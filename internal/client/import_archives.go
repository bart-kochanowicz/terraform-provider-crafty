package client

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
)

// Keep each request below common proxy body limits, including the local CI bridge.
const importChunkSize = 512 * 1024

// UploadImportArchive checks the local checksum and uploads through Crafty's
// native chunk protocol. POSTs are never retried; failed uploads may remain.
func (c *Client) UploadImportArchive(ctx context.Context, filename, expectedSHA256 string) (string, error) {
	decoded, err := hex.DecodeString(expectedSHA256)
	if err != nil || len(decoded) != sha256.Size {
		return "", fmt.Errorf("archive_sha256 must contain 64 hexadecimal characters")
	}
	file, err := os.Open(filename)
	if err != nil {
		return "", fmt.Errorf("open import archive: %w", err)
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		return "", fmt.Errorf("import archive must be a non-empty regular file")
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("hash import archive: %w", err)
	}
	if !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), expectedSHA256) {
		return "", fmt.Errorf("import archive does not match archive_sha256; nothing was uploaded")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("rewind import archive: %w", err)
	}
	fileID := rand.Text()
	name := "terraform-" + fileID + ".zip"
	total := int((info.Size() + importChunkSize - 1) / importChunkSize)
	if err := c.uploadImportPart(ctx, name, fileID, info.Size(), total, -1, nil); err != nil {
		return "", err
	}
	hash.Reset()
	buffer := make([]byte, importChunkSize)
	for index := 0; index < total; index++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		size := min(int64(importChunkSize), info.Size()-int64(index)*importChunkSize)
		part := buffer[:int(size)]
		if _, err := io.ReadFull(file, part); err != nil {
			return "", fmt.Errorf("read import archive chunk: %w", err)
		}
		_, _ = hash.Write(part)
		if err := c.uploadImportPart(ctx, name, fileID, info.Size(), total, index, part); err != nil {
			return "", err
		}
	}
	current, err := file.Stat()
	if err != nil || current.Size() != info.Size() || !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), expectedSHA256) {
		return "", fmt.Errorf("import archive changed during upload; no server was created")
	}
	return name, nil
}

func (c *Client) uploadImportPart(ctx context.Context, name, fileID string, size int64, total, index int, part []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/api/v2/servers/import/upload", bytes.NewReader(part))
	if err != nil {
		return fmt.Errorf("create archive upload request: %w", err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("type", "import")
	req.Header.Set("fileId", fileID)
	req.Header.Set("fileName", name)
	req.Header.Set("fileSize", strconv.FormatInt(size, 10))
	req.Header.Set("totalChunks", strconv.Itoa(total))
	req.Header.Set("chunked", "true")
	expectedStatus := "ok"
	if index >= 0 {
		req.Header.Set("chunkId", strconv.Itoa(index))
		hash := sha256.Sum256(part)
		req.Header.Set("chunkHash", hex.EncodeToString(hash[:]))
		expectedStatus = "partial"
		if index == total-1 {
			expectedStatus = "completed"
		}
	}
	response, err := c.sendRequest(req)
	if err != nil {
		return err
	}
	var envelope struct {
		Status string `json:"status"`
		Data   struct {
			FileID string `json:"file-id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response, &envelope); err != nil {
		return fmt.Errorf("decode archive upload response: %w", err)
	}
	if envelope.Status != expectedStatus || (index < 0 && envelope.Data.FileID != fileID) {
		return fmt.Errorf("crafty did not acknowledge the expected archive upload stage")
	}
	return nil
}
