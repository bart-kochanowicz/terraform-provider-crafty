package client

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func uploadFixture(t *testing.T, content []byte) (string, string) {
	t.Helper()
	filename := filepath.Join(t.TempDir(), "server-pack.zip")
	if err := os.WriteFile(filename, content, 0600); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(content)
	return filename, hex.EncodeToString(hash[:])
}

func TestUploadImportArchiveProtocol(t *testing.T) {
	for _, size := range []int{1, importChunkSize, importChunkSize + 17} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			content := bytes.Repeat([]byte("a"), size)
			filename, hash := uploadFixture(t, content)
			var received []byte
			name, fileID, calls := "", "", 0
			total := (size + importChunkSize - 1) / importChunkSize
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.URL.Path != "/api/v2/servers/import/upload" || r.Header.Get("Authorization") != "Bearer test-only" {
					t.Error("unexpected upload route or authentication")
				}
				if calls == 0 {
					name, fileID = r.Header.Get("fileName"), r.Header.Get("fileId")
					if filepath.Base(name) != name || !strings.HasSuffix(name, ".zip") || fileID == "" || r.Header.Get("chunkId") != "" {
						t.Error("unsafe filename or invalid initial upload")
					}
					_, _ = fmt.Fprintf(w, `{"status":"ok","data":{"file-id":%q}}`, fileID)
				} else {
					part, err := io.ReadAll(r.Body)
					if err != nil {
						t.Error(err)
					}
					digest := sha256.Sum256(part)
					if len(part) > importChunkSize || r.Header.Get("chunkHash") != hex.EncodeToString(digest[:]) || r.Header.Get("chunkId") != strconv.Itoa(calls-1) {
						t.Error("chunk order, size or checksum mismatch")
					}
					received = append(received, part...)
					status := "partial"
					if calls == total {
						status = "completed"
					}
					_, _ = fmt.Fprintf(w, `{"status":%q}`, status)
				}
				if r.Header.Get("type") != "import" || r.Header.Get("chunked") != "true" || r.Header.Get("fileSize") != strconv.Itoa(size) || r.Header.Get("totalChunks") != strconv.Itoa(total) || r.Header.Get("fileName") != name || r.Header.Get("fileId") != fileID {
					t.Error("inconsistent upload metadata")
				}
				calls++
			}))
			defer server.Close()
			got, err := New(server.URL, "test-only").UploadImportArchive(context.Background(), filename, hash)
			if err != nil || got != name || calls != total+1 || !bytes.Equal(received, content) {
				t.Fatalf("upload failed: name=%q calls=%d err=%v", got, calls, err)
			}
		})
	}
}

func TestUploadImportArchiveRejectsChecksumBeforeHTTP(t *testing.T) {
	filename, _ := uploadFixture(t, []byte("server archive"))
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("invalid archive must not be uploaded")
	}))
	defer server.Close()
	for _, hash := range []string{"invalid", strings.Repeat("0", 64)} {
		if _, err := New(server.URL, "test-only").UploadImportArchive(context.Background(), filename, hash); err == nil {
			t.Fatal("invalid checksum accepted")
		}
	}
}

func TestUploadImportArchiveDoesNotRetryOrExposeResponse(t *testing.T) {
	filename, hash := uploadFixture(t, []byte("server archive"))
	for _, tc := range []struct {
		code int
		body string
	}{
		{401, "secret-body secret-token"},
		{500, "secret-body secret-token"},
		{200, `{"status":"error","data":"secret-body secret-token"}`},
		{200, `{"status":"ok","data":{"file-id":"wrong"}}`},
		{200, `{"status":"completed"}`},
	} {
		t.Run(fmt.Sprintf("%d-%s", tc.code, tc.body), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls++
				w.WriteHeader(tc.code)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			_, err := New(server.URL, "secret-token").UploadImportArchive(context.Background(), filename, hash)
			if err == nil || calls != 1 || strings.Contains(err.Error(), "secret-token") || strings.Contains(err.Error(), "secret-body") {
				t.Fatalf("unsafe error or retry: calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestUploadImportArchiveDetectsChangedFile(t *testing.T) {
	filename, hash := uploadFixture(t, []byte("original"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("chunkId") == "" {
			if err := os.WriteFile(filename, []byte("modified"), 0600); err != nil {
				t.Error(err)
			}
			_, _ = fmt.Fprintf(w, `{"status":"ok","data":{"file-id":%q}}`, r.Header.Get("fileId"))
		} else {
			_, _ = io.WriteString(w, `{"status":"completed"}`)
		}
	}))
	defer server.Close()
	if _, err := New(server.URL, "test-only").UploadImportArchive(context.Background(), filename, hash); err == nil || !strings.Contains(err.Error(), "changed during upload") {
		t.Fatalf("changed source accepted: %v", err)
	}
}
