package cmd

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/zalando/go-keyring"
)

// TestMain owns package-wide test setup for cmd. Credential resolution now
// includes stored OAuth sessions, so use an in-memory keyring to keep a
// developer's real browser session from changing command test results.
func TestMain(m *testing.M) {
	keyring.MockInit()
	body, err := fetchSkillBody(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "fetching the agent skill for TestSkill*: %v\n", err)
		os.Exit(1)
	}
	agentSkillBody = body
	os.Exit(m.Run())
}
