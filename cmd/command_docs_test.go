package cmd

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestCommandDocumentationCoverage(t *testing.T) {
	root := Root()

	var walk func(*cobra.Command)
	walk = func(command *cobra.Command) {
		if command.Hidden {
			return
		}
		docs, ok := commandDocs(command)
		if !ok {
			t.Errorf("%s has no structured documentation", command.CommandPath())
		} else if len(docs.Samples) == 0 {
			t.Errorf("%s has no documentation samples", command.CommandPath())
		}

		hasRepresentativeOutput := false
		for _, sample := range docs.Samples {
			if strings.TrimSpace(sample.Invocation) == "" {
				t.Errorf("%s has a sample without an invocation", command.CommandPath())
			}
			if sample.Stdout == "none" || sample.Stderr == "none" {
				t.Errorf("%s encodes a fake none stream instead of an empty value", command.CommandPath())
			}
			if strings.TrimSpace(sample.Stdout) != "" || strings.TrimSpace(sample.Stderr) != "" {
				hasRepresentativeOutput = true
			}
			if sample.ExitStatus < 0 || sample.ExitStatus > 7 {
				t.Errorf("%s has undocumented exit status %d", command.CommandPath(), sample.ExitStatus)
			}
			if sample.ExitStatus != 0 && strings.TrimSpace(sample.Notes) == "" {
				t.Errorf("%s nonzero sample %q does not explain its exit behavior",
					command.CommandPath(), sample.Invocation)
			}
			if sample.ExternalBehavior != "" &&
				(sample.Stdout != "" || sample.Stderr != "" || sample.ExitStatus != 0 || sample.Notes != "") {
				t.Errorf("%s external sample %q mixes external behavior with CLI-owned claims", command.CommandPath(), sample.Invocation)
			}
		}

		if command.Runnable() {
			if !hasRepresentativeOutput && !strings.HasPrefix(command.CommandPath(), "axilio completion ") {
				t.Errorf("%s has no representative stdout or stderr", command.CommandPath())
			}
		}
		for _, child := range command.Commands() {
			walk(child)
		}
	}
	walk(root)
}

func TestStructuredJSONSamplesAreValidJSON(t *testing.T) {
	root := Root()
	var walk func(*cobra.Command)
	walk = func(command *cobra.Command) {
		docs, ok := commandDocs(command)
		if ok {
			for _, sample := range docs.Samples {
				if !strings.Contains(sample.Invocation, "-o json") &&
					!strings.Contains(sample.Invocation, "--output json") {
					continue
				}
				if strings.TrimSpace(sample.Stdout) == "" {
					t.Errorf("%s JSON sample %q has no stdout", command.CommandPath(), sample.Invocation)
					continue
				}
				var decoded any
				if err := json.Unmarshal([]byte(sample.Stdout), &decoded); err != nil {
					t.Errorf("%s JSON sample %q is invalid: %v\n%s",
						command.CommandPath(), sample.Invocation, err, sample.Stdout)
				}
			}
		}
		for _, child := range command.Commands() {
			walk(child)
		}
	}
	walk(root)
}

func TestFangRendersStructuredExamples(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("__FANG_TEST_WIDTH", "120")

	help := renderHelp(t, "phone", "tap", "--help")
	for _, want := range []string{
		`axilio phone tap --query "the search box"`,
		`# stdout: none`,
		`# stderr: Tapped "the search box" at 540,620`,
		`# exit status: 0`,
	} {
		if !strings.Contains(help, want) {
			t.Errorf("phone tap help missing %q\n%s", want, help)
		}
	}
}

func TestFangPinnedNonTTYColorPrecedence(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("CLICOLOR_FORCE", "1")
	t.Setenv("TERM", "xterm")
	t.Setenv("__FANG_TEST_WIDTH", "120")

	// Fang v1.0.0 delegates to colorprofile v0.3.3. Its non-TTY path lets
	// CLICOLOR_FORCE re-enable ANSI even when NO_COLOR is true; the manual must
	// describe the behavior we actually ship rather than the library's broader
	// precedence comment.
	help := renderHelpRaw(t, "phone", "tap", "--help")
	if !ansiPattern.MatchString(help) {
		t.Fatalf("pinned Fang precedence changed: expected forced ANSI with non-TTY NO_COLOR output\n%s", help)
	}
}

func TestPhoneObserveWalkthroughSurvivesFangWidths(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	for _, width := range []string{"80", "120"} {
		t.Run(width, func(t *testing.T) {
			t.Setenv("__FANG_TEST_WIDTH", width)
			help := renderHelp(t, "phone", "observe", "--help")
			for _, line := range strings.Split(phoneObserveWalkthrough, "\n") {
				if line == "" {
					continue
				}
				if !strings.Contains(help, line) {
					t.Errorf("%s-column help changed walkthrough line %q\n%s", width, line, help)
				}
			}
		})
	}

	command := findCommand(t, Root(), "phone observe")
	docs, ok := commandDocs(command)
	if !ok {
		t.Fatal("phone observe has no structured documentation")
	}
	prose := commandLongWithoutWalkthrough(command, docs)
	if strings.Contains(prose, phoneObserveWalkthrough) {
		t.Fatal("walkthrough was not separated from command prose")
	}
}
