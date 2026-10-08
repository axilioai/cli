package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestAssetForPlatform(t *testing.T) {
	ext := ".tar.gz"
	if runtime.GOOS == "windows" {
		ext = ".zip"
	}
	rel := &Release{Assets: []Asset{
		{Name: "cli_1.0.0_" + runtime.GOOS + "_" + runtime.GOARCH + ext, URL: "match"},
		{Name: "cli_1.0.0_plan9_mips.tar.gz", URL: "other"},
		{Name: "checksums.txt", URL: "cs"},
	}}
	a, ok := assetForPlatform(rel)
	if !ok {
		t.Fatalf("no asset picked for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	if a.URL != "match" {
		t.Fatalf("picked %q (%s), want the current-platform asset", a.URL, a.Name)
	}
	if _, ok := checksumsAsset(rel); !ok {
		t.Fatal("checksums.txt not found")
	}
}

func TestExtractFromTarGz(t *testing.T) {
	want := []byte("#!/bin/true\nfake axilio binary\n")
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	writeTar(t, tw, "README.md", []byte("decoy")) // must be skipped
	writeTar(t, tw, "axilio", want)
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}

	got, err := extractBinary(buf.Bytes(), "cli_1.0.0_linux_amd64.tar.gz")
	if err != nil {
		t.Fatalf("extractBinary: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("extracted %q, want %q", got, want)
	}
}

func TestExtractBinaryNotFound(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	writeTar(t, tw, "not-the-binary", []byte("x"))
	_ = tw.Close()
	_ = gz.Close()
	if _, err := extractBinary(buf.Bytes(), "cli_1.0.0_linux_amd64.tar.gz"); err == nil {
		t.Fatal("expected an error when the binary is absent from the archive")
	}
}

func TestChecksumFor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "abc123  cli_1.0.0_linux_amd64.tar.gz\ndef456  cli_1.0.0_darwin_arm64.tar.gz\n")
	}))
	t.Cleanup(srv.Close)

	got, err := checksumFor(context.Background(), srv.URL, "cli_1.0.0_darwin_arm64.tar.gz")
	if err != nil {
		t.Fatalf("checksumFor: %v", err)
	}
	if got != "def456" {
		t.Fatalf("checksum = %q, want def456", got)
	}
	if _, err := checksumFor(context.Background(), srv.URL, "cli_1.0.0_absent_arch.tar.gz"); err == nil {
		t.Fatal("expected an error for an asset with no checksum line")
	}
}

func TestIsReleaseVersion(t *testing.T) {
	for v, want := range map[string]bool{"0.1.0": true, "v1.2.3": true, "dev": false, "": false, "unknown": false} {
		if got := IsReleaseVersion(v); got != want {
			t.Fatalf("IsReleaseVersion(%q) = %v, want %v", v, got, want)
		}
	}
}

func TestIsHomebrewExecutable(t *testing.T) {
	for _, tc := range []struct {
		name string
		path string
		want bool
	}{
		{name: "Apple Silicon cask", path: "/opt/homebrew/Caskroom/axilio/0.6.1/axilio", want: true},
		{name: "Intel cask", path: "/usr/local/Caskroom/axilio/0.6.1/axilio", want: true},
		{name: "formula", path: "/opt/homebrew/Cellar/axilio/0.6.1/bin/axilio", want: true},
		{name: "curl prefix", path: "/usr/local/bin/axilio", want: false},
		{name: "name only", path: "/tmp/Caskroom-like/axilio", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := isHomebrewExecutable(tc.path); got != tc.want {
				t.Fatalf("isHomebrewExecutable(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}

func writeTar(t *testing.T, tw *tar.Writer, name string, data []byte) {
	t.Helper()
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(data))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(data); err != nil {
		t.Fatal(err)
	}
}

func TestRemoveOldBinaries(t *testing.T) {
	tests := []struct {
		name    string
		dir     bool
		removed bool
	}{
		{name: ".axilio.exe.old", removed: true},                                  // minio's fixed name, from older CLIs
		{name: ".axilio.exe.0123456789abcdef.old", removed: true},                 // an upgrade's suffix
		{name: ".axilio.exe.0123456789abcdef0123456789abcdef.old", removed: true}, // install.ps1's GUID
		{name: ".axilio.exe.new"},                                                 // minio's staging file
		{name: ".axilio.exe.abc.old"},                                             // hex, but no tool writes this length
		{name: ".axilio.exe.ABCDEF0123456789.old"},                                // upper case, which neither tool writes (no lower-case twin: macOS and Windows names ignore case)
		{name: ".axilio.exe.mine.old"},
		{name: ".axilio.exe..old"},
		{name: ".axilio.exe.0123456789abcdef.old.bak"},
		{name: ".axilio.exe.fedcba9876543210.old", dir: true}, // a directory, never removed
		{name: "axilio.exe"},
		{name: "notes.old"},
	}
	dir := t.TempDir()
	for _, tt := range tests {
		path := filepath.Join(dir, tt.name)
		var err error
		if tt.dir {
			err = os.Mkdir(path, 0o755)
		} else {
			err = os.WriteFile(path, []byte("x"), 0o644)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := removeOldBinaries(dir, "axilio.exe"); err != nil {
		t.Fatalf("removeOldBinaries: %v", err)
	}
	for _, tt := range tests {
		_, err := os.Stat(filepath.Join(dir, tt.name))
		if removed := errors.Is(err, fs.ErrNotExist); removed != tt.removed {
			t.Errorf("%s: removed = %v, want %v (stat err %v)", tt.name, removed, tt.removed, err)
		}
	}
}

func TestReplaceExecutableWindowsUsesAFreshBackupName(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "axilio.exe")
	if err := os.WriteFile(target, []byte("v1"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := replaceExecutable(bytes.NewReader([]byte("v2")), target, "windows"); err != nil {
		t.Fatalf("replaceExecutable: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "v2" {
		t.Fatalf("target = %q, want v2", got)
	}
	// The displaced binary is kept under a per-upgrade name, never the fixed one.
	matches, err := filepath.Glob(filepath.Join(dir, ".axilio.exe.*.old"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 || !isOldBinary(filepath.Base(matches[0]), "axilio.exe") || filepath.Base(matches[0]) == ".axilio.exe.old" {
		t.Fatalf("backups = %v, want one per-upgrade .axilio.exe.<suffix>.old", matches)
	}
}
