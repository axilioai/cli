package cmd

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestCommandDocumentationCoverage(t *testing.T) {
	if got := len(applicationCommandDocumentation); got != applicationCommandCount {
		t.Fatalf("structured application-documentation entries = %d, want %d", got, applicationCommandCount)
	}
	root := Root()
	wantGenerated := map[string]bool{
		"axilio help":                  false,
		"axilio completion bash":       false,
		"axilio completion zsh":        false,
		"axilio completion fish":       false,
		"axilio completion powershell": false,
	}
	runnable := 0
	visited := 0

	var walk func(*cobra.Command)
	walk = func(command *cobra.Command) {
		if command.Hidden {
			return
		}
		visited++
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
			if !strings.Contains(command.Example, sample.Invocation) {
				t.Errorf("%s Fang examples omit %q", command.CommandPath(), sample.Invocation)
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
			runnable++
			if !hasRepresentativeOutput && !strings.HasPrefix(command.CommandPath(), "axilio completion ") {
				t.Errorf("%s has no representative stdout or stderr", command.CommandPath())
			}
		}
		if _, generated := wantGenerated[command.CommandPath()]; generated {
			wantGenerated[command.CommandPath()] = true
		}
		for _, child := range command.Commands() {
			walk(child)
		}
	}
	walk(root)

	if visited != applicationCommandCount+6 {
		t.Errorf("visible command count = %d, want %d application + 6 generated", visited, applicationCommandCount)
	}
	if runnable != 47 {
		t.Errorf("visible runnable command count = %d, want 47", runnable)
	}
	for command, found := range wantGenerated {
		if !found {
			t.Errorf("generated runnable command %s was not documented", command)
		}
	}
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

func TestStructuredDocumentationOmitsRedundantWorkflowSamples(t *testing.T) {
	root := Root()
	for path, forbidden := range map[string][]string{
		"orgs use":        {"axilio orgs list"},
		"sessions stop":   {"axilio sessions list --remote", "axilio sessions stop <session-id> --yes"},
		"runs get":        {"axilio runs list", "axilio runs get <run-id>"},
		"runs cancel":     {"axilio runs list", "axilio runs cancel <run-id> --yes"},
		"api-keys delete": {"axilio api-keys list", "axilio api-keys delete <key-id> --yes"},
		"uploads push":    {"axilio uploads push <upload-id> --phone-id <phone-id>"},
		"uploads delete":  {"axilio uploads list", "axilio uploads delete <upload-id> --yes"},
	} {
		docs, ok := CommandDocs(findCommand(t, root, path))
		if !ok {
			t.Fatalf("%s has no structured documentation", path)
		}
		for _, sample := range docs.Samples {
			for _, invocation := range forbidden {
				if sample.Invocation == invocation {
					t.Errorf("%s retains redundant sample %q", path, invocation)
				}
			}
		}
	}

	create, ok := CommandDocs(findCommand(t, root, "api-keys create"))
	if !ok {
		t.Fatal("api-keys create has no structured documentation")
	}
	for _, sample := range create.Samples {
		if sample.Notes != "" {
			t.Errorf("api-keys create sample %q has non-runtime placeholder note %q", sample.Invocation, sample.Notes)
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

func TestStructuredDocumentationPreservesSelectedInvocations(t *testing.T) {
	legacy := map[string][]string{
		"axilio":           {"axilio login", `eval "$(axilio sessions start --export)"`, "axilio phone observe", `axilio phone tap --query "the search box"`},
		"api-keys":         {"axilio api-keys list", "axilio api-keys create ci", "axilio api-keys delete key_123 --yes"},
		"api-keys list":    {"axilio api-keys list", "axilio api-keys list -o json"},
		"api-keys create":  {"axilio api-keys create ci", `axilio api-keys create "release automation" -o json`},
		"api-keys delete":  {"axilio api-keys delete key_123", "axilio api-keys delete key_123 --yes"},
		"login":            {"axilio login", "axilio login --api-key axl_xxx", `printf '%s\n' "$AXILIO_API_KEY" | axilio login`},
		"logout":           {"axilio logout"},
		"status":           {"axilio status", "axilio status -o json"},
		"config":           {"axilio config", "axilio config set base-url https://api.axilio.ai", "axilio config unset base-url"},
		"config set":       {"axilio config set base-url https://api.axilio.ai"},
		"config unset":     {"axilio config unset base-url"},
		"doctor":           {"axilio doctor", "axilio doctor -o json"},
		"init":             {"axilio init", "axilio init --agent codex", "axilio init --agent claude --force"},
		"orgs":             {"axilio orgs list", "axilio orgs use example-org", "axilio --org another-org workflows list", "axilio orgs clear"},
		"orgs list":        {"axilio orgs list", "axilio orgs list -o json"},
		"orgs use":         {"axilio orgs use example-org"},
		"orgs clear":       {"axilio orgs clear"},
		"phone":            {"axilio phone observe", `axilio phone tap --query "the search box"`, `axilio phone type "Axilio"`, "axilio phone observe"},
		"phone observe":    {"axilio phone observe", "axilio phone observe --ocr-engine premium", "axilio phone observe -o json"},
		"phone find":       {`axilio phone find "the search box"`, `axilio phone find "settings icon" --ocr-engine premium`, `axilio phone find "continue button" --timeout 15s -o json`},
		"phone find-text":  {`axilio phone find-text "sign in"`, `axilio phone find-text "Sign in" --exact -o json`, `axilio phone find-text "settings" -o json`},
		"phone tap":        {`axilio phone tap --query "the search box"`, "axilio phone tap 540 1200", `axilio phone tap --session sess_123 --query "continue"`},
		"phone long-press": {"axilio phone long-press 540 1080", "axilio phone long-press 540 1080 --duration-ms 1200"},
		"phone swipe":      {"axilio phone swipe 540 1600 540 500", "axilio phone swipe 200 800 900 800 --duration-ms 500"},
		"phone type":       {`axilio phone type "hello world"`, `axilio phone type 'user@example.com'`, `axilio phone type "don't split this text"`},
		"phone key":        {"axilio phone key enter"},
		"phone screenshot": {"axilio phone screenshot", "axilio phone screenshot --out login.png"},
		"phone wait-for":   {`axilio phone wait-for "Results"`, `axilio phone wait-for "Loading" --gone`, `axilio phone wait-for "Ready" --exact --timeout 30s`},
		"phone send":       {"axilio phone send ./photo.jpg", "axilio phone send ./clip.mp4 --collection Movies", "axilio phone send ./photo.jpg --wait --timeout 2m"},
		"phones":           {"axilio phones list", "axilio phones mine"},
		"phones list":      {"axilio phones list", "axilio phones list -o json"},
		"phones mine":      {"axilio phones mine"},
		"runs":             {"axilio workflows list", "axilio runs start wf_123", "axilio runs list --workflow wf_123", "axilio runs get run_123"},
		"runs list":        {"axilio runs list", "axilio runs list --workflow wf_123 --limit 10", "axilio runs list -o json"},
		"runs start":       {"axilio runs start wf_123", "axilio runs start wf_123 --count 3", "axilio runs start wf_123 --phone-id ph_123", "axilio runs start wf_123 --start-timeout 300"},
		"runs get":         {"axilio runs get run_123", "axilio runs get run_123 -o json"},
		"runs cancel":      {"axilio runs cancel run_123", "axilio runs cancel run_123 --yes"},
		"sessions":         {"axilio sessions start", "axilio sessions list", "axilio sessions current", "axilio sessions stop sess_123"},
		"sessions list":    {"axilio sessions list", "axilio sessions list --remote", "axilio sessions list -o json"},
		"sessions current": {"axilio sessions current", "AXILIO_SESSION=sess_123 axilio sessions current", "axilio sessions current -o json"},
		"sessions start":   {"axilio sessions start", "axilio sessions start --phone-type iphone", "axilio sessions start --phone-id ph_123", "axilio sessions start --workflow wf_123", `eval "$(axilio sessions start --export)"`},
		"sessions stop":    {"axilio sessions stop sess_123", "axilio sessions stop ph_123 --yes"},
		"upgrade":          {"axilio upgrade --check", "axilio upgrade", "brew upgrade axilio"},
		"uploads":          {"axilio uploads add ./photo.jpg", "axilio uploads list", "axilio uploads push upl_123 --phone-id ph_123", "axilio uploads delete upl_123 --yes"},
		"uploads add":      {"axilio uploads add ./photo.jpg", "axilio uploads add ./asset --filename photo.jpg --mime-type image/jpeg"},
		"uploads list":     {"axilio uploads list", "axilio uploads list --search receipt --limit 20 --offset 0", "axilio uploads list --sort filename --order asc", "axilio uploads list -o json"},
		"uploads push":     {"axilio uploads push upl_123 --phone-id ph_123", "axilio uploads push upl_123 --phone-id ph_123 --collection Pictures", "axilio uploads push upl_123 --phone-id ph_123 --wait --timeout 2m"},
		"uploads delete":   {"axilio uploads delete upl_123", "axilio uploads rm upl_123 --yes"},
		"workflows":        {"axilio workflows list", "axilio workflows list --search checkout", "axilio runs start wf_123"},
		"workflows list":   {"axilio workflows list", "axilio workflows list --search checkout --limit 10", "axilio workflows list -o json"},
	}

	root := Root()
	for path, invocations := range legacy {
		command := root
		if path != root.Name() {
			command = findCommand(t, root, path)
		}
		docs, ok := CommandDocs(command)
		if !ok {
			t.Fatalf("%s has no structured documentation", path)
		}
		present := make(map[string]bool, len(docs.Samples))
		for _, sample := range docs.Samples {
			present[sample.Invocation] = true
		}
		for _, invocation := range invocations {
			if !present[invocation] {
				t.Errorf("%s lost legacy invocation %q", path, invocation)
			}
		}
	}

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
	for _, sample := range upgrade.Samples {
		if sample.Invocation != "brew upgrade axilio" {
			continue
		}
		if sample.ExternalBehavior == "" || sample.Stdout != "" || sample.Stderr != "" || sample.Notes != "" {
			t.Error("Homebrew invocation must describe external ownership without guessing process output")
		}
		if !strings.Contains(sample.ExternalBehavior, "owned by Homebrew") {
			t.Errorf("Homebrew invocation has unclear external behavior: %q", sample.ExternalBehavior)
		}
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
