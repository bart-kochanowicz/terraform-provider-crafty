package provider

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func archiveFixture(t *testing.T, entries map[string]string) (string, string) {
	t.Helper()
	filename := filepath.Join(t.TempDir(), "server-pack.zip")
	file, err := os.Create(filename)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	for name, content := range entries {
		entry, err := archive.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(data)
	return filename, hex.EncodeToString(hash[:])
}

func TestServerArchiveValidation(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		entries              map[string]string
		internal, executable string
		valid                bool
	}{
		{"root", map[string]string{"server.jar": "test"}, "", "server.jar", true},
		{"nested", map[string]string{"pack/bin/start.sh": "test"}, "pack", "bin/start.sh", true},
		{"missing executable", map[string]string{"manifest.json": "{}"}, "", "server.jar", false},
		{"traversal", map[string]string{"../server.jar": "test"}, "", "server.jar", false},
		{"absolute", map[string]string{"/server.jar": "test"}, "", "server.jar", false},
		{"windows", map[string]string{"C:\\server.jar": "test"}, "", "server.jar", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			filename, hash := archiveFixture(t, tc.entries)
			m := serverModel{ArchivePath: types.StringValue(filename), ArchiveSHA256: types.StringValue(hash), Executable: types.StringValue(tc.executable), ArchiveInternalPath: types.StringValue(tc.internal)}
			if err := m.validateSource(); err != nil {
				t.Fatal(err)
			}
			if err := m.validateArchive(); (err == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, err)
			}
		})
	}
}

func TestServerInstallationSourceValidation(t *testing.T) {
	valid := serverModel{Engine: types.StringValue("paper"), Version: types.StringValue("1.21.1")}
	if err := valid.validateSource(); err != nil {
		t.Fatal(err)
	}
	for _, m := range []serverModel{
		{},
		{Engine: types.StringValue("paper")},
		{ArchivePath: types.StringValue("pack.zip")},
		{ArchivePath: types.StringValue("pack.zip"), Engine: types.StringValue("paper")},
		{Engine: valid.Engine, Version: valid.Version, Executable: types.StringValue("server.jar")},
	} {
		if err := m.validateSource(); err == nil {
			t.Fatal("ambiguous or incomplete installation accepted")
		}
	}
	unknown := serverModel{ArchivePath: types.StringUnknown()}
	if err := unknown.validateSource(); err != nil {
		t.Fatalf("unknown archive rejected during planning: %v", err)
	}
}

func TestServerArchiveRejectsCorruptEntry(t *testing.T) {
	filename, _ := archiveFixture(t, map[string]string{"server.jar": "original-entry"})
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.Replace(data, []byte("original-entry"), []byte("modified-entry"), 1)
	if err := os.WriteFile(filename, data, 0600); err != nil {
		t.Fatal(err)
	}
	m := serverModel{ArchivePath: types.StringValue(filename), Executable: types.StringValue("server.jar")}
	if err := m.validateArchive(); err == nil {
		t.Fatal("ZIP entry with a bad CRC was accepted")
	}
}
