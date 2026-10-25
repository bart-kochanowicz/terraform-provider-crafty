package provider

import (
	"archive/zip"
	"encoding/hex"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func (m serverModel) validateSource() error {
	if m.ArchivePath.IsUnknown() {
		return nil
	}
	if m.ArchivePath.IsNull() {
		for _, value := range []types.String{m.Engine, m.Version} {
			if !value.IsUnknown() && (value.IsNull() || strings.TrimSpace(value.ValueString()) == "") {
				return fmt.Errorf("engine and version are required unless archive_path is set")
			}
		}
		if !m.ArchiveSHA256.IsNull() || !m.Executable.IsNull() || !m.ArchiveInternalPath.IsNull() {
			return fmt.Errorf("archive_sha256, archive_internal_path and executable require archive_path")
		}
		return nil
	}
	if !m.Engine.IsNull() || !m.Version.IsNull() {
		return fmt.Errorf("archive_path cannot be combined with engine or version")
	}
	if strings.TrimSpace(m.ArchivePath.ValueString()) == "" {
		return fmt.Errorf("archive_path must not be blank")
	}
	if !m.ArchiveSHA256.IsUnknown() {
		hash, err := hex.DecodeString(m.ArchiveSHA256.ValueString())
		if err != nil || len(hash) != 32 {
			return fmt.Errorf("archive_sha256 is required and must contain 64 hexadecimal characters")
		}
	}
	if !m.Executable.IsUnknown() && !safeArchivePath(m.Executable.ValueString(), false) {
		return fmt.Errorf("executable is required and must be a relative archive path without traversal")
	}
	if !m.ArchiveInternalPath.IsUnknown() && !safeArchivePath(m.ArchiveInternalPath.ValueString(), true) {
		return fmt.Errorf("archive_internal_path must be a relative directory without traversal")
	}
	return nil
}

func safeArchivePath(value string, allowRoot bool) bool {
	if value == "" || value == "." {
		return allowRoot
	}
	if path.IsAbs(value) || strings.ContainsAny(value, "\\:\x00") {
		return false
	}
	for _, part := range strings.Split(value, "/") {
		if part == ".." {
			return false
		}
	}
	return true
}

func (m serverModel) validateArchive() error {
	archive, err := zip.OpenReader(m.ArchivePath.ValueString())
	if err != nil {
		return fmt.Errorf("open ZIP server pack: %w", err)
	}
	defer func() { _ = archive.Close() }()
	executable := path.Join(m.ArchiveInternalPath.ValueString(), m.Executable.ValueString())
	found := false
	for _, file := range archive.File {
		if !safeArchivePath(file.Name, true) || (!file.Mode().IsRegular() && !file.FileInfo().IsDir()) {
			return fmt.Errorf("ZIP server pack contains an unsafe path or special file")
		}
		if file.Mode().IsRegular() {
			reader, err := file.Open()
			if err != nil {
				return fmt.Errorf("open ZIP entry: %w", err)
			}
			_, readErr := io.Copy(io.Discard, reader)
			closeErr := reader.Close()
			if readErr != nil || closeErr != nil {
				return fmt.Errorf("ZIP server pack contains a corrupt or unreadable entry")
			}
		}
		if path.Clean(file.Name) == executable && file.Mode().IsRegular() {
			found = true
		}
	}
	if !found {
		return fmt.Errorf("executable was not found inside archive_internal_path; use a server pack rather than a client pack")
	}
	return nil
}
