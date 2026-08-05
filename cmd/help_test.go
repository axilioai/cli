package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/fang"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	applicationCommandCount = 50
	applicationFlagCount    = 54
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
				"Credentials resolve in this order: --api-key, AXILIO_API_KEY",
				"each API key is scoped to the organization that created it",
				"API host resolves from --base-url, AXILIO_BASE_URL",
				"Phone command session selection precedence is --session, AXILIO_SESSION",
				"Every successful command emits valid JSON with -o json",
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
				"does not detect a stored OAuth session",
				"config unset",
				"set XDG_CONFIG_HOME; file is [value]/axilio/config.json",
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
			want: []string{"checksum-verified", "Homebrew", "returns before checking", "brew upgrade axilio"},
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
				"local lease file",
				"AXILIO_SESSION",
				"current-session pointer",
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
		"Session selection precedence is --session, AXILIO_SESSION, the sole active lease",
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
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		if !strings.Contains(rootHelp, shell) {
			t.Errorf("completion help missing %q\n%s", shell, rootHelp)
		}
		help := renderHelp(t, "completion", shell, "--help")
		if !strings.Contains(help, "--no-descriptions") {
			t.Errorf("completion %s help missing --no-descriptions\n%s", shell, help)
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
	// AXI-1507 made universal JSON success output true: every successful
	// command emits valid JSON under -o json (json_output_test.go holds the
	// line), so the README is required to say so rather than forbidden to.
	if !strings.Contains(readme, "emits valid JSON") {
		t.Error("README no longer documents universal JSON success output (AXI-1507)")
	}

	// The skill is no longer a file in this repo (AXI-1527): it's hosted at
	// the backend's /skill route, fetched once per test binary by TestMain.
	// These assertions therefore check the copy users actually receive.
	skill := agentSkillBody
	if !strings.Contains(skill, "axilio phone key enter") {
		t.Error("agent skill must show the supported enter key")
	}
	for _, contradiction := range []string{"enter, back, home", "back, home, recents"} {
		if strings.Contains(strings.ToLower(skill), contradiction) {
			t.Errorf("agent skill advertises unsupported named keys: %q", contradiction)
		}
	}
	for _, command := range []string{
		"axilio sessions start --export",
		"axilio phone tap --query",
		"axilio phone send",
		"axilio uploads list",
	} {
		if !strings.Contains(skill, command) {
			t.Errorf("agent skill missing %q", command)
		}
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
