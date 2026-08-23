package cmd

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/axilioai/cli/internal/exit"
	"github.com/charmbracelet/fang"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	applicationCommandCount = 60
	applicationFlagCount    = 80
)

func TestApplicationHelpMetadata(t *testing.T) {
	root := Root()
	commands := applicationCommands(root)
	if got := len(commands); got != applicationCommandCount {
		t.Fatalf("application command count = %d, want %d", got, applicationCommandCount)
	}

	for _, command := range commands {
		if strings.TrimSpace(command.Short) == "" {
			t.Errorf("%s has no Short help", command.CommandPath())
		}
		if strings.TrimSpace(command.Long) == "" {
			t.Errorf("%s has no Long help", command.CommandPath())
		}
		if strings.TrimSpace(command.Example) == "" {
			t.Errorf("%s has no Example help", command.CommandPath())
		}
		if strings.Contains(command.Long, ". ") && !strings.Contains(command.Long, "\n\n") {
			t.Errorf("%s has multi-sentence help without a paragraph break", command.CommandPath())
		}
		for _, line := range strings.Split(command.Example, "\n") {
			if len([]rune(strings.TrimSpace(line))) > 88 {
				t.Errorf("%s has an example wider than 88 characters: %q",
					command.CommandPath(), strings.TrimSpace(line))
			}
		}
	}
}

func TestApplicationFlagUsage(t *testing.T) {
	root := Root()
	var flags []string
	for _, command := range applicationCommands(root) {
		visitApplicationFlags(command, func(flag *pflag.Flag) {
			flags = append(flags, command.CommandPath()+" --"+flag.Name)
			usage := strings.TrimSpace(flag.Usage)
			switch {
			case len(usage) < 12:
				t.Errorf("%s has weak usage %q", flags[len(flags)-1], usage)
			case strings.EqualFold(usage, flag.Name):
				t.Errorf("%s merely repeats the flag name", flags[len(flags)-1])
			case strings.Contains(strings.ToLower(usage), "todo"),
				strings.Contains(strings.ToLower(usage), "tbd"):
				t.Errorf("%s has placeholder usage %q", flags[len(flags)-1], usage)
			}
		})
	}
	if got := len(flags); got != applicationFlagCount {
		t.Fatalf("application flag count = %d, want %d\n%s",
			got, applicationFlagCount, strings.Join(flags, "\n"))
	}
}

func TestNoEffectGlobalFlagClassification(t *testing.T) {
	root := Root()
	globalFlags := []string{"api-key", "base-url", "no-color", "org", "output", "quiet"}
	initCommand := findCommand(t, root, "init")
	for _, name := range []string{"api-key", "org"} {
		if !commandGlobalFlagHasNoEffect(initCommand, name) {
			t.Errorf("init --%s is not classified as having no effect", name)
		}
	}
	for _, name := range []string{"base-url", "no-color", "output", "quiet"} {
		if commandGlobalFlagHasNoEffect(initCommand, name) {
			t.Errorf("init --%s is incorrectly classified as having no effect", name)
		}
	}
	helpCommand := findCommand(t, root, "help")
	for _, name := range globalFlags {
		if !commandGlobalFlagHasNoEffect(helpCommand, name) {
			t.Errorf("help --%s is not classified as having no effect", name)
		}
	}
}

func TestTimeoutFlagHelpDocumentsSyntaxAndEffectiveDefault(t *testing.T) {
	root := Root()
	tests := []struct {
		path         string
		usage        string
		defaultValue string
	}{
		{path: "phone find", usage: visionTimeoutHelp, defaultValue: "10s"},
		{path: "phone wait-for", usage: ocrTimeoutHelp, defaultValue: "10s"},
		{path: "phone send", usage: deliveryTimeoutHelp, defaultValue: "1m0s"},
		{path: "uploads push", usage: deliveryTimeoutHelp, defaultValue: "1m0s"},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			command := findCommand(t, root, tc.path)
			flag := command.Flags().Lookup("timeout")
			if flag == nil {
				t.Fatalf("%s has no --timeout flag", command.CommandPath())
			}
			if got := flag.Value.Type(); got != "duration" {
				t.Errorf("%s --timeout type = %q, want duration", command.CommandPath(), got)
			}
			if flag.Usage != tc.usage {
				t.Errorf("%s --timeout usage = %q, want %q", command.CommandPath(), flag.Usage, tc.usage)
			}
			if got := flag.Value.String(); got != tc.defaultValue {
				t.Errorf("%s --timeout effective default = %q, want %q", command.CommandPath(), got, tc.defaultValue)
			}
			if flag.DefValue != "" {
				t.Errorf("%s --timeout automatic default suffix = %q; usage owns the normalized default", command.CommandPath(), flag.DefValue)
			}
		})
	}
}

func TestStartTimeoutHelpDocumentsIntegerSecondsSemantics(t *testing.T) {
	command := findCommand(t, Root(), "runs start")
	flag := command.Flags().Lookup("start-timeout")
	if flag == nil {
		t.Fatal("runs start has no --start-timeout flag")
	}
	if got := flag.Value.Type(); got != "int64" {
		t.Errorf("runs start --start-timeout type = %q, want int64", got)
	}
	if flag.Usage != startTimeoutHelp {
		t.Errorf("runs start --start-timeout usage = %q, want %q", flag.Usage, startTimeoutHelp)
	}
	if got := flag.Value.String(); got != "0" {
		t.Errorf("runs start --start-timeout default = %q, want 0", got)
	}
	if strings.Contains(flag.Usage, durationUnitsHelp) {
		t.Error("runs start --start-timeout incorrectly advertises duration suffixes instead of whole seconds")
	}
}

func TestRenderedHelpSnapshots(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("__FANG_TEST_WIDTH", "120")

	for _, tc := range []struct {
		name string
		args []string
		file string
	}{
		{name: "root", args: []string{"--help"}, file: "root.txt"},
		{name: "phone tap", args: []string{"phone", "tap", "--help"}, file: "phone-tap.txt"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := renderHelp(t, tc.args...)
			assertGolden(t, filepath.Join("testdata", "help", tc.file), got)
		})
	}
}

func TestRenderedHelpContracts(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("__FANG_TEST_WIDTH", "120")

	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "root",
			args: []string{"--help"},
			want: []string{
				"For detailed offline documentation, run man axilio",
				"axilio help --html",
				"Credentials resolve in this order: --api-key, AXILIO_API_KEY",
				"each API key is scoped to the organization that created it",
				"API host resolves from --base-url, AXILIO_BASE_URL",
				"Phone command session selection precedence is --session, AXILIO_SESSION",
				"Every successful runnable application command emits exactly one JSON document with -o json",
				"Built-in help, completion, and version remain text",
				"Warnings and errors also use stderr, but remain visible in every mode",
				"Destructive commands prompt only when stdin is a terminal",
			},
		},
		{
			name: "login",
			args: []string{"login", "--help"},
			want: []string{"browser OAuth", "--api-key axl_xxx", "printf '%s\\n'"},
		},
		{
			name: "config",
			args: []string{"config", "--help"},
			want: []string{
				"matching browser-session authentication summary",
				"status` verifies that the effective credentials are usable",
				"config unset",
				"set XDG_CONFIG_HOME; file is [value]/axilio/config.json",
				"directory is [value]/axilio/sessions",
				"axilio config set base-url",
				"axilio orgs use [org_name]",
			},
		},
		{
			name: "organizations",
			args: []string{"orgs", "--help"},
			want: []string{"Swap between the organizations", "OAuth", "API keys are bound to one org", "AXILIO_ORG", "orgs clear"},
		},
		{
			name: "init",
			args: []string{"init", "--help"},
			want: []string{
				".claude/skills/axilio/SKILL.md",
				"AGENTS.md",
				".cursor/rules/axilio.mdc",
				"--force",
			},
		},
		{
			name: "upgrade",
			args: []string{"upgrade", "--help"},
			want: []string{"checksum-verified", "including for Homebrew-managed installations", "Check for a newer release without installing it", "brew upgrade axilio"},
		},
		{
			name: "phones",
			args: []string{"phones", "--help"},
			want: []string{
				"shared",
				"dedicated",
				"phones mine",
				"sessions start --phone-id",
			},
		},
		{
			name: "sessions",
			args: []string{"sessions", "--help"},
			want: []string{
				"Sessions remain active in Axilio until stopped",
				"saves connection information locally",
				"AXILIO_SESSION",
				"most recently started session",
				"sessions list --remote",
				"Show the session currently selected for phone commands",
			},
		},
		{
			name: "phone",
			args: []string{"phone", "--help"},
			want: []string{
				"observe",
				"find-text",
				"long-press",
				"screenshot",
				"wait-for",
				"send",
			},
		},
		{
			name: "phone type",
			args: []string{"phone", "type", "--help"},
			want: []string{
				"Type text into the focused field",
				"shell-special characters",
				"US-layout keyboard",
				"Printable ASCII characters are supported",
				"emoji and other non-ASCII characters are silently skipped",
			},
		},
		{
			name: "phone screenshot",
			args: []string{"phone", "screenshot", "--help"},
			want: []string{
				"contents are overwritten without confirmation",
				"does not create a backup",
				"overwrite existing contents without confirmation",
			},
		},
		{
			name: "workflows",
			args: []string{"workflows", "--help"},
			want: []string{
				"workflows list",
				"runs start",
				"sessions start --workflow",
			},
		},
		{
			name: "runs",
			args: []string{"runs", "--help"},
			want: []string{
				"workflows list",
				"runs start",
				"runs get",
				"cancel",
			},
		},
		{
			name: "runs start",
			args: []string{"runs", "start", "--help"},
			want: []string{
				"must be between 1 and 1000, inclusive",
				"Number of run configurations to create (1-1000)",
				"--count must be between 1 and 1000 (got 0)",
				"range is validated before credentials, allocation, or an API request",
			},
		},
		{
			name: "sessions current",
			args: []string{"sessions", "current", "--help"},
			want: []string{
				"exits with not-found status 4",
				"all output modes leave stdout empty",
				"selected session keeps the same JSON shape",
			},
		},
		{
			name: "api keys",
			args: []string{"api-keys", "--help"},
			want: []string{
				"organization",
				"shown once",
				"api-keys delete",
			},
		},
		{
			name: "uploads",
			args: []string{"uploads", "--help"},
			want: []string{
				"add",
				"list",
				"push",
				"delete",
				"phone send",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := renderHelp(t, tc.args...)
			contract := contractText(got)
			for _, want := range tc.want {
				if !strings.Contains(contract, want) {
					t.Errorf("rendered help missing %q\n%s", want, got)
				}
			}
		})
	}
}

func TestPhoneTapRenderedContract(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("__FANG_TEST_WIDTH", "120")

	got := renderHelp(t, "phone", "tap", "--help")
	contract := contractText(got)
	for _, want := range []string{
		`axilio phone tap --query "the search box"`,
		"axilio phone tap 540 1200",
		"Perform a tap action on the selected phone",
		"Use --query to find an element by natural-language description and tap its center",
		"frame-space pixels",
		"top-left",
		"--query takes precedence",
		"Session selection precedence is --session, AXILIO_SESSION, the only locally saved session",
		"No effect; the session's embedded control token in the websocket URL authenticates phone commands",
		"TAP FLAGS",
		"PHONE FLAGS",
		"GLOBAL FLAGS",
	} {
		if !strings.Contains(contract, want) {
			t.Errorf("phone tap help missing %q\n%s", want, got)
		}
	}

	tapFlags := sectionBetween(t, got, "TAP FLAGS", "PHONE FLAGS")
	for _, flag := range []string{"--help", "--model", "--ocr-engine", "--query"} {
		if !strings.Contains(tapFlags, flag) {
			t.Errorf("TAP FLAGS missing %s\n%s", flag, got)
		}
	}
	phoneFlags := sectionBetween(t, got, "PHONE FLAGS", "GLOBAL FLAGS")
	if !strings.Contains(phoneFlags, "--session") {
		t.Errorf("PHONE FLAGS missing --session\n%s", got)
	}
	globalFlags := sectionBetween(t, got, "GLOBAL FLAGS", "")
	for _, flag := range []string{"--api-key", "--base-url", "--no-color", "--org", "--output", "--quiet"} {
		if !strings.Contains(globalFlags, flag) {
			t.Errorf("GLOBAL FLAGS missing %s\n%s", flag, got)
		}
	}
}

func TestPhoneTapRenderedHelpPreservesColor(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("CLICOLOR_FORCE", "1")
	t.Setenv("__FANG_TEST_WIDTH", "120")

	got := renderHelpRaw(t, "phone", "tap", "--help")
	if !ansiPattern.MatchString(got) {
		t.Fatalf("phone tap help lost ANSI styling\n%s", got)
	}
	plain := normalizeHelp(got)
	for _, section := range []string{"TAP FLAGS", "PHONE FLAGS", "GLOBAL FLAGS"} {
		if !strings.Contains(plain, section) {
			t.Errorf("colored help missing %q\n%s", section, plain)
		}
	}
}

func TestRenderedHelpUsesFlagOwnershipSections(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("__FANG_TEST_WIDTH", "120")

	root := Root()
	var commands []*cobra.Command
	var walk func(*cobra.Command)
	walk = func(command *cobra.Command) {
		for _, child := range command.Commands() {
			if child.Hidden {
				continue
			}
			commands = append(commands, child)
			walk(child)
		}
	}
	walk(root)

	for _, command := range commands {
		command := command
		t.Run(command.CommandPath(), func(t *testing.T) {
			args := strings.Fields(command.CommandPath())[1:]
			args = append(args, "--help")
			rendered := renderHelp(t, args...)
			if splitFlag := wrappedFlagNamePattern.FindString(rendered); splitFlag != "" {
				t.Errorf("rendered help splits a flag name across lines: %q\n%s", splitFlag, rendered)
			}
			got := contractText(rendered)
			for _, section := range []string{
				strings.ToUpper(command.Name()) + " FLAGS",
				"GLOBAL FLAGS",
			} {
				if !strings.Contains(got, section) {
					t.Errorf("rendered help missing %q\n%s", section, got)
				}
			}
			for parent := command.Parent(); parent != nil && parent != root; parent = parent.Parent() {
				if parent.PersistentFlags().HasAvailableFlags() {
					section := strings.ToUpper(parent.Name()) + " FLAGS"
					if !strings.Contains(got, section) {
						t.Errorf("rendered help missing inherited %q\n%s", section, got)
					}
				}
			}
			for _, name := range []string{"api-key", "base-url", "no-color", "org", "output", "quiet"} {
				usage, ok := commandGlobalFlagUsage(command, name)
				if !ok {
					t.Errorf("no expected --%s help registered", name)
					continue
				}
				if !strings.Contains(strings.ToLower(got), strings.ToLower(contractText(usage))) {
					t.Errorf("rendered help missing command-specific --%s usage %q\n%s", name, usage, rendered)
				}
			}
		})
	}
}

func findCommand(t *testing.T, root *cobra.Command, path string) *cobra.Command {
	t.Helper()
	command, _, err := root.Find(strings.Fields(path))
	if err != nil || command == nil {
		t.Fatalf("find command %q: %v", path, err)
	}
	return command
}

func TestHelpCaptureWriterPreservesFileDescriptor(t *testing.T) {
	const fd = uintptr(42)
	var buffer bytes.Buffer
	writer := helpCaptureWriter(&buffer, descriptorWriter{Buffer: &bytes.Buffer{}, fd: fd})
	file, ok := writer.(interface{ Fd() uintptr })
	if !ok {
		t.Fatal("terminal help capture does not expose a file descriptor")
	}
	if got := file.Fd(); got != fd {
		t.Fatalf("terminal help capture fd = %d, want %d", got, fd)
	}
}

type descriptorWriter struct {
	*bytes.Buffer
	fd uintptr
}

func (writer descriptorWriter) Fd() uintptr { return writer.fd }

func TestGeneratedCompletionHelp(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("__FANG_TEST_WIDTH", "120")

	rootHelp := renderHelp(t, "completion", "--help")
	normalizedRootHelp := strings.Join(strings.Fields(rootHelp), " ")
	for _, want := range []string{
		"axilio api-<Tab>",
		"axilio phone <Tab>",
		"axilio sessions start --<Tab>",
		"complete command names, subcommands, and flags",
		"resource IDs and names are not fetched dynamically",
	} {
		if !strings.Contains(normalizedRootHelp, want) {
			t.Errorf("completion help missing %q\n%s", want, rootHelp)
		}
	}
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		if !strings.Contains(rootHelp, shell) {
			t.Errorf("completion help missing %q\n%s", shell, rootHelp)
		}
		help := renderHelp(t, "completion", shell, "--help")
		if !strings.Contains(help, "--no-descriptions") {
			t.Errorf("completion %s help missing --no-descriptions\n%s", shell, help)
		}
		if strings.Contains(help, "__axilio_debug") || strings.Contains(help, "#compdef axilio") {
			t.Errorf("completion %s help embeds generated script output\n%s", shell, help)
		}
	}
}

func TestDocumentationHelpParity(t *testing.T) {
	readme := readTestFile(t, filepath.Join("..", "README.md"))
	for _, command := range []string{
		"`phone send`",
		"`runs start`",
		"`workflows list`",
		"`uploads add`",
		"`uploads list`",
		"`uploads push`",
		"`uploads delete`",
	} {
		if !strings.Contains(readme, command) {
			t.Errorf("README command index missing %s", command)
		}
	}
	for _, completion := range []string{
		"completion bash",
		"completion zsh",
		"completion fish",
		"completion powershell",
		"--no-descriptions",
	} {
		if !strings.Contains(readme, completion) {
			t.Errorf("README completion docs missing %q", completion)
		}
	}
	// JSON success is one document for runnable application commands. Cobra's
	// built-in help/completion and version paths remain text, while --export is
	// a distinct shell contract that rejects JSON.
	normalizedReadme := strings.Join(strings.Fields(readme), " ")
	if !strings.Contains(normalizedReadme,
		"successful runnable application commands emit one JSON document under `-o json`") {
		t.Error("README no longer documents runnable-command JSON success output")
	}
	if !strings.Contains(normalizedReadme,
		"`sessions start --export` is a separate exact shell contract and rejects JSON") {
		t.Error("README no longer documents the sessions export/JSON conflict")
	}
	if !strings.Contains(normalizedReadme,
		"Built-in help and completion commands, bare parent-command help, `--help`, and `--version` remain text") {
		t.Error("README no longer documents the non-JSON built-in output paths")
	}

	// The README table is a deliberately terser summary of exit.Codes, which
	// the manual renders in full. Only the set of codes has to agree; adding
	// or removing one without touching the README is the drift that matters.
	for _, documented := range exit.Codes {
		row := fmt.Sprintf("| `%d` |", documented.Code)
		if !strings.Contains(readme, row) {
			t.Errorf("README exit-status table has no row for code %d (%s)",
				documented.Code, documented.Name)
		}
	}
	codeRows := regexp.MustCompile(`(?m)^\s*\|\s*`+"`"+`\d+`+"`"+`\s*\|`).FindAllString(readme, -1)
	if got, want := len(codeRows), len(exit.Codes); got != want {
		t.Errorf("README documents %d exit codes, exit.Codes has %d", got, want)
	}

}

func applicationCommands(root *cobra.Command) []*cobra.Command {
	var commands []*cobra.Command
	var walk func(*cobra.Command)
	walk = func(command *cobra.Command) {
		if command.Name() == "completion" || command.Name() == "help" || command.Name() == "man" {
			return
		}
		commands = append(commands, command)
		for _, child := range command.Commands() {
			walk(child)
		}
	}
	walk(root)
	slices.SortFunc(commands, func(a, b *cobra.Command) int {
		return strings.Compare(a.CommandPath(), b.CommandPath())
	})
	return commands
}

func visitApplicationFlags(command *cobra.Command, visit func(*pflag.Flag)) {
	seen := map[string]bool{}
	for _, set := range []*pflag.FlagSet{command.LocalNonPersistentFlags(), command.PersistentFlags()} {
		set.VisitAll(func(flag *pflag.Flag) {
			if flag.Name == "help" || flag.Name == "version" || seen[flag.Name] {
				return
			}
			seen[flag.Name] = true
			visit(flag)
		})
	}
}

func renderHelp(t *testing.T, args ...string) string {
	t.Helper()
	return normalizeHelp(renderHelpRaw(t, args...))
}

func renderHelpRaw(t *testing.T, args ...string) string {
	t.Helper()
	root := Root()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(args)
	if err := fang.Execute(context.Background(), root, fang.WithoutVersion()); err != nil {
		t.Fatalf("render help %q: %v\nstderr:\n%s", strings.Join(args, " "), err, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("render help %q wrote stderr:\n%s", strings.Join(args, " "), stderr.String())
	}
	return stdout.String()
}

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;:?]*[ -/]*[@-~]`)

func normalizeHelp(value string) string {
	value = ansiPattern.ReplaceAllString(value, "")
	value = strings.ReplaceAll(value, "\r\n", "\n")
	lines := strings.Split(value, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " \t")
	}
	return strings.TrimSpace(strings.Join(lines, "\n")) + "\n"
}

var wrappedHyphenPattern = regexp.MustCompile(`-\n[ \t]+`)
var wrappedFlagNamePattern = regexp.MustCompile(`--(?:[a-z0-9-]*-)?[ \t]*\n[ \t]+[a-z0-9]`)

func contractText(value string) string {
	value = wrappedHyphenPattern.ReplaceAllString(value, "-")
	return strings.Join(strings.Fields(value), " ")
}

func sectionBetween(t *testing.T, value, start, end string) string {
	t.Helper()
	startAt := strings.Index(value, start)
	if startAt < 0 {
		t.Fatalf("rendered help missing section %q\n%s", start, value)
	}
	value = value[startAt+len(start):]
	if end == "" {
		return value
	}
	endAt := strings.Index(value, end)
	if endAt < 0 {
		t.Fatalf("rendered help missing section %q after %q\n%s", end, start, value)
	}
	return value[:endAt]
}

func assertGolden(t *testing.T, path, got string) {
	t.Helper()
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want := readTestFile(t, path)
	if got != want {
		t.Fatalf("rendered help differs from %s; run UPDATE_GOLDEN=1 go test ./cmd\n--- want\n%s\n--- got\n%s",
			path, want, got)
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	value, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(value)
}
