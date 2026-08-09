package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/axilioai/cli/internal/exit"
)

// sparseFileOfSize creates a temp file of exactly size bytes without writing
// them (truncate; the filesystem stores it sparse), so the 100 MiB boundary
// costs no real I/O.
func sparseFileOfSize(t *testing.T, size int64) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "payload.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := f.Truncate(size); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return path
}

// The preflight refuses an undeliverable file before `phone send` resolves a
// session or dials anywhere (AXI-1581): without it, the one-shot command
// uploaded the file into the org library and only then had the delivery
// refused, retaining the upload and consuming quota for a failed command.
// The boundary is inclusive (exactly 100 MiB is deliverable), matching the
// server's pinned _maxDeliveryBytes. Unreadable paths and directories are
// deliberately not this check's business — they keep their existing errors.
func TestOversizeForDelivery(t *testing.T) {
	dir := t.TempDir()

	cases := []struct {
		name     string
		path     func(t *testing.T) string
		wantCode exit.Code
	}{
		{
			name:     "small file passes",
			path:     func(t *testing.T) string { return sparseFileOfSize(t, 1) },
			wantCode: exit.OK,
		},
		{
			name:     "exactly 100 MiB passes (inclusive boundary)",
			path:     func(t *testing.T) string { return sparseFileOfSize(t, maxDeliveryBytes) },
			wantCode: exit.OK,
		},
		{
			name:     "one byte over is usage-refused",
			path:     func(t *testing.T) string { return sparseFileOfSize(t, maxDeliveryBytes+1) },
			wantCode: exit.Usage,
		},
		{
			name:     "missing path is not this check's business",
			path:     func(t *testing.T) string { return filepath.Join(dir, "absent.png") },
			wantCode: exit.OK,
		},
		{
			name:     "directory is not this check's business",
			path:     func(t *testing.T) string { return dir },
			wantCode: exit.OK,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := oversizeForDelivery(tc.path(t))
			if tc.wantCode == exit.OK {
				if err != nil {
					t.Fatalf("preflight refused: %v", err)
				}
				return
			}
			if got := exit.Classify(err); got != tc.wantCode {
				t.Fatalf("exit code = %d, want %d (err: %v)", got, tc.wantCode, err)
			}
		})
	}
}
