package cmd

import (
	"os"
	"testing"

	"github.com/zalando/go-keyring"
)

// TestMain owns package-wide test setup for cmd. Credential resolution now
// includes stored OAuth sessions, so use an in-memory keyring to keep a
// developer's real browser session from changing command test results.
func TestMain(m *testing.M) {
	keyring.MockInit()
	os.Exit(m.Run())
}
