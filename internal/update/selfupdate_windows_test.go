package update

import (
	"bytes"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/minio/selfupdate"
)

// holdOpen keeps path open without delete sharing, the way Windows holds the
// image of a running executable: it can be read but not deleted or replaced.
func holdOpen(t *testing.T, path string) {
	t.Helper()
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	h, err := syscall.CreateFile(p, syscall.GENERIC_READ, syscall.FILE_SHARE_READ, nil,
		syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.CloseHandle(h) })
}

// AXI-2225: the binary displaced by an earlier upgrade is still running (an
// open `axilio runs watch`) when the next upgrade starts.
func TestReplaceExecutableWhileAnOlderBackupIsRunning(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "axilio.exe")
	if err := os.WriteFile(target, []byte("v2"), 0o755); err != nil {
		t.Fatal(err)
	}
	running := filepath.Join(dir, ".axilio.exe.old")
	if err := os.WriteFile(running, []byte("v1"), 0o755); err != nil {
		t.Fatal(err)
	}
	holdOpen(t, running)

	// minio's default backup name is the occupied one, so it cannot upgrade.
	if err := selfupdate.Apply(bytes.NewReader([]byte("v3")), selfupdate.Options{TargetPath: target}); err == nil {
		t.Fatal("default selfupdate.Apply succeeded over a running .axilio.exe.old; the scenario no longer reproduces")
	}

	if err := replaceExecutable(bytes.NewReader([]byte("v3")), target, "windows"); err != nil {
		t.Fatalf("replaceExecutable: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "v3" {
		t.Fatalf("target = %q, want v3", got)
	}
	// The running copy is left alone, to be swept by a later upgrade.
	if _, err := os.Stat(running); err != nil {
		t.Fatalf("running backup was removed: %v", err)
	}
}
