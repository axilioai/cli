package cmd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestGenerateManpageDeterministicAndComplete(t *testing.T) {
	version := readManpageTestVersion(t)
	first, err := GenerateManpage(Root(), version)
	if err != nil {
		t.Fatal(err)
	}
	second, err := GenerateManpage(Root(), version)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("manpage generation is not deterministic across fresh Root calls")
	}

	root := Root()
	markdown, err := generateManpageMarkdown(root, version)
	if err != nil {
		t.Fatal(err)
	}
	for _, section := range []string{
		"# NAME {#NAME}\n",
		"# SYNOPSIS {#SYNOPSIS}\n",
		"# DESCRIPTION {#DESCRIPTION}\n",
		"# COMMON WORKFLOW {#COMMON_WORKFLOW}\n",
		"# GLOBAL OPTIONS {#GLOBAL_OPTIONS}\n",
		"# COMMANDS {#COMMANDS}\n",
		"# ENVIRONMENT {#ENVIRONMENT}\n",
		"# EXIT STATUS {#EXIT_STATUS}\n",
		"# FILES {#FILES}\n",
		"# NOTES {#NOTES}\n",
		"# EXAMPLES {#EXAMPLES}\n",
		"# SEE ALSO {#SEE_ALSO}\n",
	} {
		if !strings.Contains(markdown, section) {
			t.Errorf("generated Markdown missing %q", strings.TrimSpace(section))
		}
	}
	for _, command := range publicCommands(root) {
		anchor := commandHTMLAnchor(root, command)
		section, ok := commandMarkdownSection(markdown, anchor)
		if !ok {
			t.Errorf("generated Markdown missing public command %q", command.CommandPath())
			continue
		}
		useLine := command.UseLine()
		if !strings.Contains(section, useLine) && !strings.Contains(section, strings.TrimSuffix(useLine, " [flags]")) {
			t.Errorf("command section %q is missing use line %q", command.CommandPath(), useLine)
		}
		if command.Runnable() && !strings.Contains(section, "**Examples and expected output**") {
			t.Errorf("runnable command %q has no expected-output section", command.CommandPath())
		}
		if !command.Runnable() && strings.Contains(section, "**Examples and expected output**") {
			t.Errorf("non-runnable command %q claims expected output", command.CommandPath())
		}
	}
	for _, ordered := range [][]string{
		{"## api-keys {#COMMAND_api-keys}", "### list {#COMMAND_api-keys-list}", "### create {#COMMAND_api-keys-create}", "### delete {#COMMAND_api-keys-delete}"},
		{"## completion {#COMMAND_completion}", "### bash {#COMMAND_completion-bash}", "### fish {#COMMAND_completion-fish}", "### powershell {#COMMAND_completion-powershell}", "### zsh {#COMMAND_completion-zsh}"},
	} {
		last := -1
		for _, heading := range ordered {
			position := strings.Index(markdown, heading)
			if position < 0 {
				t.Errorf("generated Markdown missing grouped heading %q", heading)
				continue
			}
			if position <= last {
				t.Errorf("grouped heading %q is out of order", heading)
			}
			last = position
		}
	}
	commonWorkflow := strings.SplitN(markdown, "# COMMON WORKFLOW {#COMMON_WORKFLOW}\n", 2)[1]
	commonWorkflow = strings.SplitN(commonWorkflow, "# GLOBAL OPTIONS {#GLOBAL_OPTIONS}\n", 2)[0]
	rootDocs, ok := CommandDocs(root)
	if !ok {
		t.Fatal("root command has no structured documentation")
	}
	for _, sample := range rootDocs.Samples {
		if !strings.Contains(commonWorkflow, sample.Invocation) {
			t.Errorf("common workflow missing root invocation %q", sample.Invocation)
		}
	}
	observeDocs, ok := CommandDocs(findManpageTestCommand(t, root, "phone observe"))
	if !ok || strings.TrimSpace(observeDocs.Walkthrough) == "" {
		t.Fatal("phone observe has no structured walkthrough")
	}
	if got := strings.Count(markdown, strings.TrimSpace(observeDocs.Walkthrough)); got != 1 {
		t.Errorf("phone observe walkthrough occurs %d times, want exactly once", got)
	}
	if strings.Contains(markdown, "```\nnone\n```") {
		t.Error("an explicit none stream was rendered as literal command output")
	}
	if !bytes.Contains(first, []byte(`.TH "AXILIO" "1"`)) {
		t.Errorf("roff header is not an AXILIO(1) title:\n%s", first[:min(len(first), 400)])
	}
	for _, entity := range []string{"&lt;", "&gt;", "&amp;"} {
		if bytes.Contains(first, []byte(entity)) {
			t.Errorf("roff output leaked Markdown entity %q", entity)
		}
	}
	for _, literal := range []string{"<session-id>", "source <(axilio completion bash)", "->"} {
		if !bytes.Contains(first, []byte(literal)) {
			t.Errorf("roff output lost literal %q", literal)
		}
	}
}

func commandMarkdownSection(markdown, anchor string) (string, bool) {
	marker := "{#" + anchor + "}\n"
	start := strings.Index(markdown, marker)
	if start < 0 {
		return "", false
	}
	section := markdown[start+len(marker):]
	if next := strings.Index(section, "{#COMMAND_"); next >= 0 {
		section = section[:next]
	}
	return section, true
}

func TestGeneratedManpageKeepsCompletionReferenceFocused(t *testing.T) {
	markdown, err := generateManpageMarkdown(Root(), readManpageTestVersion(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"COMMAND_completion",
		"COMMAND_completion-bash",
		"COMMAND_completion-fish",
		"COMMAND_completion-powershell",
		"COMMAND_completion-zsh",
	} {
		section, ok := commandMarkdownSection(markdown, path)
		if !ok {
			t.Errorf("%s section not found", path)
			continue
		}
		if strings.Contains(section, "Global-option behavior") {
			t.Errorf("%s repeats unrelated global-option behavior", path)
		}
	}
	for _, unwanted := range []string{"__axilio_debug", "#compdef axilio"} {
		if strings.Contains(markdown, unwanted) {
			t.Errorf("manpage embeds generated completion script excerpt %q", unwanted)
		}
	}
}

func TestGeneratedManpageMatchesCheckedInPage(t *testing.T) {
	generated, err := GenerateManpage(Root(), readManpageTestVersion(t))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("..", "man", "axilio.1")
	checkedIn, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v; run `go generate ./...`", path, err)
	}
	if !bytes.Equal(generated, checkedIn) {
		t.Fatalf("%s is stale; run `go generate ./...`", path)
	}
}

func TestGeneratedManpageUsesEffectiveGlobalFlagHelp(t *testing.T) {
	root := Root()
	markdown, err := generateManpageMarkdown(root, readManpageTestVersion(t))
	if err != nil {
		t.Fatal(err)
	}
	command, _, err := root.Find([]string{"phone", "tap"})
	if err != nil {
		t.Fatal(err)
	}
	normalized := strings.Join(strings.Fields(markdown), " ")
	if strings.Contains(normalized, "human-readable the ") {
		t.Errorf("manpage contains malformed effective flag help: human-readable the ...")
	}
	for _, name := range []string{"api-key", "base-url", "no-color", "org", "output", "quiet"} {
		usage, ok := commandGlobalFlagUsage(command, name)
		if !ok {
			t.Fatalf("phone tap has no effective --%s help", name)
		}
		if commandGlobalFlagHasNoEffect(command, name) {
			continue
		}
		if !strings.Contains(normalized, strings.Join(strings.Fields(usage), " ")) {
			t.Errorf("manpage does not reuse effective --%s help %q", name, usage)
		}
	}
}

func TestGeneratedManpageGroupsNoEffectGlobalFlagsFirst(t *testing.T) {
	markdown, err := generateManpageMarkdown(Root(), readManpageTestVersion(t))
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		anchor      string
		group       string
		firstDetail string
	}{
		{
			anchor: "COMMAND_help",
			group: "**--api-key**, **--base-url**, **--no-color**, **--org**, **--output** / **-o**, **--quiet** / **-q** - " +
				"No effect on help command or the help content it renders",
		},
		{
			anchor: "COMMAND_phone-tap",
			group: "**--api-key**, **--base-url**, **--no-color**, **--org** - " +
				"No effect on phone tap command",
			firstDetail: "**-o**, **--output**=*value* - Emit a human confirmation or JSON tap result",
		},
	}

	for _, tc := range tests {
		t.Run(tc.anchor, func(t *testing.T) {
			section, ok := commandMarkdownSection(markdown, tc.anchor)
			if !ok {
				t.Fatalf("%s section not found", tc.anchor)
			}
			normalized := strings.Join(strings.Fields(section), " ")
			groupAt := strings.Index(normalized, tc.group)
			if groupAt < 0 {
				t.Fatalf("%s is missing grouped no-effect flags %q\n%s", tc.anchor, tc.group, section)
			}
			behaviorAt := strings.Index(normalized, "Global-option behavior")
			if behaviorAt < 0 || groupAt < behaviorAt {
				t.Fatalf("%s grouped row is not in Global-option behavior\n%s", tc.anchor, section)
			}
			if tc.firstDetail != "" {
				detailAt := strings.Index(normalized, tc.firstDetail)
				if detailAt < 0 {
					t.Fatalf("%s is missing effective flag detail %q\n%s", tc.anchor, tc.firstDetail, section)
				}
				if groupAt > detailAt {
					t.Fatalf("%s lists effective flag details before the no-effect group\n%s", tc.anchor, section)
				}
			}
		})
	}
}

func TestManpageExternalSampleDoesNotGuessProcessContract(t *testing.T) {
	var rendered strings.Builder
	writeSample(&rendered, externalSample(
		"brew upgrade axilio",
		"Output and exit status are owned by Homebrew, not the axilio CLI.",
	))
	got := rendered.String()
	if !strings.Contains(got, "```console\nuser@host ~ % brew upgrade axilio") ||
		!strings.Contains(got, "# External behavior:") ||
		!strings.Contains(got, "owned by Homebrew") {
		t.Fatalf("external behavior is missing:\n%s", got)
	}
	for _, forbidden := range []string{"**Standard output**", "# Standard error:", "# Exit status:"} {
		if strings.Contains(got, forbidden) {
			t.Errorf("external sample guessed %s:\n%s", forbidden, got)
		}
	}
}

func TestManpageSampleUsesOneTerminalTranscript(t *testing.T) {
	var rendered strings.Builder
	writeSample(&rendered, CommandSample{
		Invocation: "axilio doctor",
		Stdout:     "CHECK | STATUS | DETAIL\nAuth  | ok     | ready",
		ExitStatus: 0,
		Notes:      "Representative report.",
	})
	got := rendered.String()
	for _, want := range []string{
		"```console\nuser@host ~ % axilio doctor\n",
		"CHECK | STATUS | DETAIL",
		"# Exit status: 0",
		"# Notes: Representative report.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("terminal transcript missing %q:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{"**Invocation**", "**Standard output**", "**Standard error**", "None."} {
		if strings.Contains(got, unwanted) {
			t.Errorf("terminal transcript retains separate field %q:\n%s", unwanted, got)
		}
	}
}

func TestManpageSampleLabelsNonemptyStderrInsideTranscript(t *testing.T) {
	var rendered strings.Builder
	writeSample(&rendered, CommandSample{
		Invocation: "axilio login",
		Stderr:     "Opening browser",
		ExitStatus: 0,
	})
	got := rendered.String()
	if !strings.Contains(got, "user@host ~ % axilio login\n\n# Standard error:\nOpening browser") {
		t.Fatalf("stderr is not identified inside terminal transcript:\n%s", got)
	}
}

func TestGenerateManpageProtectsLiteralRoffInput(t *testing.T) {
	root := &cobra.Command{Use: "axilio", Short: "Test command", Long: "Test command."}
	child := AttachCommandDocumentation(&cobra.Command{
		Use:   "literal",
		Short: "Print difficult literal input",
		Long:  "Print difficult literal input.",
		RunE:  func(*cobra.Command, []string) error { return nil },
	}, CommandDocumentation{Samples: []CommandSample{{
		Invocation: "axilio literal",
		Stdout:     ".leading-control\n'apostrophe-control\nback\\slash",
	}}})
	root.AddCommand(child)

	rendered, err := GenerateManpage(root, "test")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "literal.1")
	if err := os.WriteFile(path, rendered, 0o600); err != nil {
		t.Fatal(err)
	}
	mandoc, err := exec.LookPath("mandoc")
	if err != nil {
		t.Skip("mandoc not installed")
	}
	out, err := exec.Command(mandoc, "-Tutf8", path).CombinedOutput()
	if err != nil {
		t.Fatalf("render literal manpage: %v\n%s", err, out)
	}
	for _, literal := range []string{".leading-control", "'apostrophe-control", "back\\slash"} {
		if !bytes.Contains(out, []byte(literal)) {
			t.Errorf("rendered manpage lost literal %q\n%s", literal, out)
		}
	}
}

func TestGeneratedManpageMandocClean(t *testing.T) {
	mandoc, err := exec.LookPath("mandoc")
	if err != nil {
		t.Skip("mandoc not installed")
	}
	rendered, err := GenerateManpage(Root(), readManpageTestVersion(t))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "axilio.1")
	if err := os.WriteFile(path, rendered, 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(mandoc, "-Tlint", path).CombinedOutput()
	if err != nil || len(out) != 0 {
		t.Fatalf("mandoc -Tlint must exit successfully with no diagnostics: %v\n%s", err, out)
	}
}

func TestGeneratedManpagePreservesPhoneObserveWalkthrough(t *testing.T) {
	mandoc, err := exec.LookPath("mandoc")
	if err != nil {
		t.Skip("mandoc not installed")
	}
	rendered, err := GenerateManpage(Root(), readManpageTestVersion(t))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "axilio.1")
	if err := os.WriteFile(path, rendered, 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(mandoc, "-Tutf8", path).CombinedOutput()
	if err != nil {
		t.Fatalf("render manpage: %v\n%s", err, out)
	}
	for _, literal := range []string{
		"+----------------------------+",
		"540,1120",
		"axilio phone tap 540 1120",
	} {
		if !bytes.Contains(out, []byte(literal)) {
			t.Errorf("rendered walkthrough lost %q", literal)
		}
	}
}

func TestGenerateManpageRejectsInvalidInputs(t *testing.T) {
	if _, err := GenerateManpage(nil, "0.7.0"); err == nil {
		t.Error("nil root did not fail")
	}
	if _, err := GenerateManpage(Root(), "\n"); err == nil {
		t.Error("empty version did not fail")
	}
	if _, err := GenerateManpage(Root(), "bad\nversion"); err == nil {
		t.Error("version containing a newline did not fail")
	}
}

func readManpageTestVersion(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "VERSION"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(b))
}

func findManpageTestCommand(t *testing.T, root *cobra.Command, path string) *cobra.Command {
	t.Helper()
	command, _, err := root.Find(strings.Fields(path))
	if err != nil || command == nil {
		t.Fatalf("find command %q: %v", path, err)
	}
	return command
}
