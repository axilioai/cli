package cmd

import (
	"os"
	"strings"
	"testing"

	"github.com/axilioai/cli/internal/exit"
	"github.com/zalando/go-keyring"
)

// isolateCreds keeps init's sign-in check away from the developer's real
// keychain and config, so tests exercise the signed-out path deterministically.
func isolateCreds(t *testing.T) {
	t.Helper()
	keyring.MockInit()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("AXILIO_API_KEY", "")
}

func TestInitWritesSkillForEachAgent(t *testing.T) {
	cases := []struct{ agent, path string }{
		{"claude", ".claude/skills/axilio/SKILL.md"},
		{"cursor", ".cursor/rules/axilio.mdc"},
		{"codex", "AGENTS.md"},
	}
	for _, c := range cases {
		t.Run(c.agent, func(t *testing.T) {
			t.Chdir(t.TempDir())
			isolateCreds(t)
			if err := runInit(t.Context(), c.agent, false); err != nil {
				t.Fatalf("init %s: %v", c.agent, err)
			}
			b, err := os.ReadFile(c.path)
			if err != nil {
				t.Fatalf("%s not written: %v", c.path, err)
			}
			s := string(b)
			// Both SDK sections reach every target: the agent picks the language at
			// run time, so a target missing one can't honor the user's choice.
			// (That the symbols inside them are real is agentskill_test.go's job.)
			for _, want := range []string{"<!-- lang:python -->", "<!-- lang:go -->"} {
				if !strings.Contains(s, want) {
					t.Fatalf("%s missing %s", c.path, want)
				}
			}
			// The stamp is what makes a stale skill detectable on disk.
			if !strings.Contains(s, "<!-- axilio skill ") {
				t.Fatalf("%s missing the version stamp", c.path)
			}
			if !strings.Contains(s, "--agent "+c.agent+" --force") {
				t.Fatalf("%s stamp does not name its own refresh command", c.path)
			}
		})
	}
}

func TestInitUnknownAgentIsUsageError(t *testing.T) {
	t.Chdir(t.TempDir())
	isolateCreds(t)
	err := runInit(t.Context(), "emacs", false)
	if err == nil || exit.Classify(err) != exit.Usage {
		t.Fatalf("expected usage error, got %v", err)
	}
}

// AGENTS.md is shared, so codex must append without clobbering, stay idempotent,
// and refresh (not duplicate) under --force.
func TestInitCodexAppendsSafely(t *testing.T) {
	t.Chdir(t.TempDir())
	isolateCreds(t)
	if err := os.WriteFile("AGENTS.md", []byte("# My Project\n\nkeep me\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runInit(t.Context(), "codex", false); err != nil {
		t.Fatal(err)
	}
	s := readFile(t, "AGENTS.md")
	if !strings.Contains(s, "keep me") {
		t.Fatal("append clobbered existing content")
	}
	if strings.Count(s, agentsMarkerBegin) != 1 {
		t.Fatalf("want exactly one axilio block, got %d", strings.Count(s, agentsMarkerBegin))
	}

	// Re-run without --force is a usage error, not a duplicate.
	if err := runInit(t.Context(), "codex", false); err == nil || exit.Classify(err) != exit.Usage {
		t.Fatalf("expected usage error on re-run, got %v", err)
	}

	// --force refreshes the one block and preserves the user's content.
	if err := runInit(t.Context(), "codex", true); err != nil {
		t.Fatal(err)
	}
	s = readFile(t, "AGENTS.md")
	if strings.Count(s, agentsMarkerBegin) != 1 || !strings.Contains(s, "keep me") {
		t.Fatal("force refresh duplicated the block or lost content")
	}
}

func TestInitExistingFileWithoutForce(t *testing.T) {
	t.Chdir(t.TempDir())
	isolateCreds(t)
	if err := runInit(t.Context(), "claude", false); err != nil {
		t.Fatal(err)
	}
	if err := runInit(t.Context(), "claude", false); err == nil || exit.Classify(err) != exit.Usage {
		t.Fatalf("expected usage error on existing file, got %v", err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Bare init auto-detects agents from repo markers and writes one skill per hit.
func TestInitAutoDetectsAgents(t *testing.T) {
	t.Chdir(t.TempDir())
	isolateCreds(t)
	if err := os.Mkdir(".claude", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("AGENTS.md", []byte("# My Project\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := runInit(t.Context(), "", false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(".claude/skills/axilio/SKILL.md"); err != nil {
		t.Fatalf("claude skill not written: %v", err)
	}
	if s := readFile(t, "AGENTS.md"); !strings.Contains(s, agentsMarkerBegin) {
		t.Fatal("AGENTS.md did not gain the axilio block")
	}
	// No .cursor marker, so no cursor rule.
	if _, err := os.Stat(".cursor"); !os.IsNotExist(err) {
		t.Fatal(".cursor should not have been created")
	}
}

// Auto mode skips targets that already carry the skill instead of erroring the
// whole run; only an explicit --agent hard-errors on an existing file.
func TestInitAutoSkipsExistingWithoutForce(t *testing.T) {
	t.Chdir(t.TempDir())
	isolateCreds(t)
	if err := os.Mkdir(".claude", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := runInit(t.Context(), "", false); err != nil {
		t.Fatal(err)
	}
	before := readFile(t, ".claude/skills/axilio/SKILL.md")

	if err := runInit(t.Context(), "", false); err != nil {
		t.Fatalf("auto re-run should skip, not error: %v", err)
	}
	if readFile(t, ".claude/skills/axilio/SKILL.md") != before {
		t.Fatal("auto re-run rewrote the skill without --force")
	}
}

// With no markers and no terminal, bare init is a usage error that names the
// escape hatch — never a hang on stdin.
func TestInitNoMarkersNonTTYIsUsageError(t *testing.T) {
	t.Chdir(t.TempDir())
	isolateCreds(t)
	err := runInit(t.Context(), "", false)
	if err == nil || exit.Classify(err) != exit.Usage {
		t.Fatalf("expected usage error, got %v", err)
	}
	if !strings.Contains(err.Error(), "--agent") {
		t.Fatalf("error should name --agent as the escape hatch: %v", err)
	}
}
