package cmd

import (
	"os"
	"strings"
	"testing"

	"github.com/axilioai/cli/internal/exit"
)

func TestInitWritesSkillForEachAgent(t *testing.T) {
	cases := []struct{ agent, path string }{
		{"claude", ".claude/skills/axilio/SKILL.md"},
		{"cursor", ".cursor/rules/axilio.mdc"},
		{"codex", "AGENTS.md"},
	}
	for _, c := range cases {
		t.Run(c.agent, func(t *testing.T) {
			t.Chdir(t.TempDir())
			if err := runInit(c.agent, false); err != nil {
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
	err := runInit("emacs", false)
	if err == nil || exit.Classify(err) != exit.Usage {
		t.Fatalf("expected usage error, got %v", err)
	}
}

// AGENTS.md is shared, so codex must append without clobbering, stay idempotent,
// and refresh (not duplicate) under --force.
func TestInitCodexAppendsSafely(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("AGENTS.md", []byte("# My Project\n\nkeep me\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runInit("codex", false); err != nil {
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
	if err := runInit("codex", false); err == nil || exit.Classify(err) != exit.Usage {
		t.Fatalf("expected usage error on re-run, got %v", err)
	}

	// --force refreshes the one block and preserves the user's content.
	if err := runInit("codex", true); err != nil {
		t.Fatal(err)
	}
	s = readFile(t, "AGENTS.md")
	if strings.Count(s, agentsMarkerBegin) != 1 || !strings.Contains(s, "keep me") {
		t.Fatal("force refresh duplicated the block or lost content")
	}
}

func TestInitExistingFileWithoutForce(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := runInit("claude", false); err != nil {
		t.Fatal(err)
	}
	if err := runInit("claude", false); err == nil || exit.Classify(err) != exit.Usage {
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
