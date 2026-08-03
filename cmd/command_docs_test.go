package cmd

import (
	"encoding/json"
	"fmt"
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
		docs, ok := CommandDocs(command)
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

func TestCompletionDocumentationModelsInstallationInsteadOfGeneratedScript(t *testing.T) {
	root := Root()
	for _, path := range []string{
		"completion bash",
		"completion zsh",
		"completion fish",
		"completion powershell",
	} {
		command := findCommand(t, root, path)
		docs, ok := CommandDocs(command)
		if !ok || len(docs.Samples) != 1 {
			t.Fatalf("%s setup samples = %#v, documented=%v", path, docs.Samples, ok)
		}
		sample := docs.Samples[0]
		if sample.Stdout != "" || sample.Stderr != "" {
			t.Errorf("%s exposes generated-script output instead of a consumed installation example: %#v", path, sample)
		}
		if !strings.Contains(sample.Invocation, "axilio completion ") {
			t.Errorf("%s setup invocation does not run completion: %q", path, sample.Invocation)
		}
		if sample.Notes == "" {
			t.Errorf("%s setup invocation does not explain its effect", path)
		}
	}
}

func TestStructuredJSONSamplesAreValidJSON(t *testing.T) {
	root := Root()
	var walk func(*cobra.Command)
	walk = func(command *cobra.Command) {
		docs, ok := CommandDocs(command)
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

func TestDoctorJSONSampleMatchesHumanTable(t *testing.T) {
	docs, ok := CommandDocs(findCommand(t, Root(), "doctor"))
	if !ok {
		t.Fatal("doctor has no structured documentation")
	}
	var human, jsonOutput string
	for _, sample := range docs.Samples {
		switch sample.Invocation {
		case "axilio doctor":
			human = sample.Stdout
		case "axilio doctor -o json":
			jsonOutput = sample.Stdout
			if strings.Contains(sample.Notes, "Shortened") {
				t.Errorf("doctor JSON sample is still described as shortened: %q", sample.Notes)
			}
		}
	}
	if human == "" || jsonOutput == "" {
		t.Fatalf("doctor samples incomplete: human=%v json=%v", human != "", jsonOutput != "")
	}

	var result struct {
		Checks []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
			Detail string `json:"detail"`
		} `json:"checks"`
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal([]byte(jsonOutput), &result); err != nil {
		t.Fatalf("decode doctor JSON sample: %v", err)
	}
	if !result.OK {
		t.Error("successful doctor JSON sample has ok=false")
	}
	if got, want := len(strings.Split(human, "\n"))-1, len(result.Checks); got != want {
		t.Errorf("doctor table data rows = %d, JSON checks = %d", got, want)
	}
	for _, check := range result.Checks {
		row := fmt.Sprintf("%-15s | %-6s | %s", check.Name, check.Status, check.Detail)
		if !strings.Contains(human, row) {
			t.Errorf("doctor table does not contain JSON check row %q", row)
		}
	}
}

func TestCommandDocumentationIsPerTree(t *testing.T) {
	first := Root()
	second := Root()

	firstStatus := findCommand(t, first, "status")
	secondStatus := findCommand(t, second, "status")
	firstStatus.Annotations[commandDocumentationAnnotation] = "broken"

	if _, ok := CommandDocs(firstStatus); ok {
		t.Fatal("invalid command annotation decoded successfully")
	}
	if _, ok := CommandDocs(secondStatus); !ok {
		t.Fatal("mutating one Root tree changed another Root tree")
	}
}

func TestStructuredDocumentationPreservesSpecialProcessContracts(t *testing.T) {
	root := Root()
	sessionsStart, _ := CommandDocs(findCommand(t, root, "sessions start"))
	foundBareExport := false
	foundEval := false
	for _, sample := range sessionsStart.Samples {
		switch sample.Invocation {
		case "axilio sessions start --export":
			foundBareExport = true
			if sample.Stdout != "export AXILIO_SESSION=<session-id>" {
				t.Errorf("bare --export stdout = %q", sample.Stdout)
			}
		case `eval "$(axilio sessions start --export)"`:
			foundEval = true
			if sample.Stdout != "" || !strings.Contains(sample.Notes, "sets AXILIO_SESSION") {
				t.Errorf("eval sample must model consumed stdout and its shell side effect: %#v", sample)
			}
		}
	}
	if !foundBareExport || !foundEval {
		t.Errorf("sessions start export samples incomplete: bare=%v eval=%v", foundBareExport, foundEval)
	}
	upgrade, _ := CommandDocs(findCommand(t, root, "upgrade"))
	foundHomebrew := false
	for _, sample := range upgrade.Samples {
		if sample.Invocation != "brew upgrade axilio" {
			continue
		}
		foundHomebrew = true
		if sample.ExternalBehavior == "" || sample.Stdout != "" || sample.Stderr != "" || sample.Notes != "" {
			t.Error("Homebrew invocation must describe external ownership without guessing process output")
		}
		if !strings.Contains(sample.ExternalBehavior, "owned by Homebrew") {
			t.Errorf("Homebrew invocation has unclear external behavior: %q", sample.ExternalBehavior)
		}
	}
	if !foundHomebrew {
		t.Error("upgrade documentation has no Homebrew-owned invocation")
	}
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
	docs, ok := CommandDocs(command)
	if !ok {
		t.Fatal("phone observe has no structured documentation")
	}
	prose := commandLongWithoutWalkthrough(command, docs)
	if strings.Contains(prose, phoneObserveWalkthrough) {
		t.Fatal("walkthrough was not separated from command prose")
	}
}
