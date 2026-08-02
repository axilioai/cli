package cmd

//go:generate go run ./manpage --version-file ../VERSION ../man/axilio.1

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/axilioai/cli/internal/exit"
	"github.com/cpuguy83/go-md2man/v2/md2man"
	"github.com/russross/blackfriday/v2"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	manpageWrapWidth = 72
	// TH requires a parseable date for a diagnostic-free mandoc lint. Keep the
	// source date fixed instead of introducing a wall-clock generation input.
	manpageSourceDate = "2026-08-02"
)

// GenerateManpage renders one comprehensive axilio(1) page from a freshly
// constructed Cobra command tree. The output depends only on the tree and the
// supplied source version; it never includes the wall clock or host state.
func GenerateManpage(root *cobra.Command, version string) ([]byte, error) {
	markdown, err := generateValidatedManpageMarkdown(root, version)
	if err != nil {
		return nil, err
	}
	renderer := md2man.NewRoffRenderer()
	roff := bytes.TrimRight(blackfriday.Run(
		[]byte(markdown),
		blackfriday.WithRenderer(renderer),
		blackfriday.WithExtensions(renderer.GetExtensions()|blackfriday.HeadingIDs),
	), "\n")
	return append(roff, '\n'), nil
}

func generateValidatedManpageMarkdown(root *cobra.Command, version string) (string, error) {
	if root == nil {
		return "", errors.New("generate manpage: nil root command")
	}
	version = strings.TrimSpace(version)
	if version == "" {
		return "", errors.New("generate manpage: empty version")
	}
	if strings.ContainsAny(version, "\r\n\"") {
		return "", fmt.Errorf("generate manpage: invalid version %q", version)
	}
	return generateManpageMarkdown(root, version)
}

func generateManpageMarkdown(root *cobra.Command, version string) (string, error) {
	root.InitDefaultHelpCmd()
	root.InitDefaultCompletionCmd()
	root.InitDefaultHelpFlag()
	root.InitDefaultVersionFlag()

	commands := publicCommands(root)
	if len(commands) == 0 {
		return "", errors.New("generate manpage: command tree has no public commands")
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%% \"AXILIO\" \"1\" \"%s\" \"axilio %s\" \"Axilio CLI Manual\"\n", manpageSourceDate, version)
	writeManpageHeading(&b, 1, "NAME", "NAME")
	writeWrapped(&b, root.Name()+" - "+strings.TrimSuffix(root.Short, "."))

	writeManpageHeading(&b, 1, "SYNOPSIS", "SYNOPSIS")
	writeLiteral(&b, root.Name()+" [global options] <command> [command options]")

	overview, resolution, outputBehavior := splitManpageRootDescription(firstNonempty(root.Long, root.Short))
	writeManpageHeading(&b, 1, "DESCRIPTION", "DESCRIPTION")
	writeDescription(&b, overview)
	writeWrapped(&b, "Resolution precedence and output-mode details are collected under NOTES.")

	writeManpageHeading(&b, 1, "COMMON WORKFLOW", "COMMON_WORKFLOW")
	writeWrapped(&b, "A typical interactive session signs in, acquires a phone, observes it, performs an action, verifies the result, and releases the session:")
	if docs, ok := CommandDocs(root); ok && len(docs.Samples) > 0 {
		invocations := make([]string, 0, len(docs.Samples))
		for _, sample := range docs.Samples {
			invocations = append(invocations, sample.Invocation)
		}
		writeLiteral(&b, strings.Join(invocations, "\n"))
	} else if strings.TrimSpace(root.Example) != "" {
		writeLiteral(&b, deindent(root.Example))
	} else {
		writeLiteral(&b, "axilio login\naxilio sessions start\naxilio phone observe\naxilio sessions stop <session-id>")
	}
	writeWrapped(&b, "Use `axilio sessions start --export` with `eval` when separate shells or agents must pin different sessions through `AXILIO_SESSION`.")

	writeManpageHeading(&b, 1, "GLOBAL OPTIONS", "GLOBAL_OPTIONS")
	writeFlagList(&b, root, rootGlobalFlags(root), false)

	writeManpageHeading(&b, 1, "COMMANDS", "COMMANDS")
	writeWrapped(&b, "Commands are grouped by their top-level family. Each runnable command includes representative output and exit behavior from its structured documentation.")
	for _, command := range publicCommandChildren(root) {
		writeCommandTree(&b, root, command, 2)
	}

	writeManpageHeading(&b, 1, "ENVIRONMENT", "ENVIRONMENT")
	writeEnvironment(&b)

	writeManpageHeading(&b, 1, "EXIT STATUS", "EXIT_STATUS")
	writeExitStatuses(&b)

	writeManpageHeading(&b, 1, "FILES", "FILES")
	writeFiles(&b)

	writeManpageHeading(&b, 1, "NOTES", "NOTES")
	writeManpageHeading(&b, 2, "Resolution precedence", "NOTES_resolution_precedence")
	writeDescription(&b, resolution)
	writeManpageHeading(&b, 2, "Output behavior", "NOTES_output_behavior")
	writeDescription(&b, outputBehavior)

	writeManpageHeading(&b, 1, "EXAMPLES", "EXAMPLES")
	writeWrapped(&b, "Observe and act on a selected phone session:")
	writeLiteral(&b, "axilio sessions start --phone-type android\naxilio phone observe\naxilio phone tap --query \"the search box\"\naxilio phone type \"coffee shops\"\naxilio phone observe")
	writeWrapped(&b, "Start independent sessions in separate shells:")
	writeLiteral(&b, "eval \"$(axilio sessions start --export)\"\naxilio phone screenshot --out screenshot.png")
	writeWrapped(&b, "Run a workflow and request machine-readable output:")
	writeLiteral(&b, "axilio workflows list -o json\naxilio runs start <workflow-id> --count 2 -o json")

	writeManpageHeading(&b, 1, "SEE ALSO", "SEE_ALSO")
	writeWrapped(&b, "Axilio documentation: https://docs.axilio.ai")
	writeWrapped(&b, "CLI issue tracker: https://github.com/axilioai/cli/issues")
	writeWrapped(&b, "Axilio product site: https://axilio.ai")

	return b.String(), nil
}

func writeCommandTree(b *strings.Builder, root, command *cobra.Command, level int) {
	writeManpageHeading(b, min(level, 6), command.Name(), commandHTMLAnchor(root, command))
	writeCommandReference(b, root, command)
	for _, child := range publicCommandChildren(command) {
		writeCommandTree(b, root, child, level+1)
	}
}

func writeCommandReference(b *strings.Builder, root, command *cobra.Command) {
	writeLiteral(b, command.UseLine())
	docs, hasDocs := CommandDocs(command)
	description := firstNonempty(command.Long, command.Short)
	if hasDocs {
		description = commandLongWithoutWalkthrough(command, docs)
	}
	writeDescription(b, description)

	local := commandLocalFlags(command)
	if len(local) > 0 {
		writeManpageLabel(b, "Command options")
		writeFlagList(b, command, local, true)
	}

	inherited := commandInheritedFlags(root, command)
	if len(inherited) > 0 {
		writeManpageLabel(b, "Parent options")
		writeFlagList(b, command, inherited, true)
	}

	writeManpageLabel(b, "Global-option behavior")
	writeFlagList(b, command, rootGlobalFlags(root), true)

	if hasDocs && len(docs.Samples) > 0 {
		if command.Runnable() {
			writeManpageLabel(b, "Examples and expected output")
			for i, sample := range docs.Samples {
				if len(docs.Samples) > 1 {
					fmt.Fprintf(b, "**Sample %d**\n\n", i+1)
				}
				writeSample(b, sample)
			}
		} else {
			writeManpageLabel(b, "Workflow examples")
			invocations := make([]string, 0, len(docs.Samples))
			for _, sample := range docs.Samples {
				invocations = append(invocations, sample.Invocation)
			}
			writeLiteral(b, strings.Join(invocations, "\n"))
		}
	} else if strings.TrimSpace(command.Example) != "" {
		writeManpageLabel(b, "Examples")
		writeLiteral(b, deindent(command.Example))
	}
	if hasDocs && strings.TrimSpace(docs.Walkthrough) != "" {
		writeManpageLabel(b, "Walkthrough")
		writeLiteral(b, docs.Walkthrough)
	}
}

func writeManpageHeading(b *strings.Builder, level int, label, id string) {
	fmt.Fprintf(b, "%s %s {#%s}\n", strings.Repeat("#", level), label, id)
}

func writeManpageLabel(b *strings.Builder, label string) {
	fmt.Fprintf(b, "**%s**\n\n", label)
}

func writeSample(b *strings.Builder, sample CommandSample) {
	var transcript strings.Builder
	fmt.Fprintf(&transcript, "user@host ~ %% %s\n", strings.TrimSpace(sample.Invocation))
	if strings.TrimSpace(sample.ExternalBehavior) != "" {
		transcript.WriteByte('\n')
		writeTerminalComment(&transcript, "External behavior", sample.ExternalBehavior)
		if strings.TrimSpace(sample.Notes) != "" {
			writeTerminalComment(&transcript, "Notes", sample.Notes)
		}
		writeTerminalLiteral(b, transcript.String())
		return
	}

	if !streamIsNone(sample.Stdout) {
		transcript.WriteString(strings.TrimSpace(sample.Stdout))
		transcript.WriteByte('\n')
	}

	if !streamIsNone(sample.Stderr) {
		transcript.WriteByte('\n')
		transcript.WriteString("# Standard error:\n")
		transcript.WriteString(strings.TrimSpace(sample.Stderr))
		transcript.WriteByte('\n')
	}

	transcript.WriteByte('\n')
	writeTerminalComment(&transcript, "Exit status", strconv.Itoa(sample.ExitStatus))
	if strings.TrimSpace(sample.Notes) != "" {
		writeTerminalComment(&transcript, "Notes", sample.Notes)
	}
	writeTerminalLiteral(b, transcript.String())
}

func writeTerminalComment(out *strings.Builder, label, value string) {
	prefix := "# " + label + ": "
	continuation := "# " + strings.Repeat(" ", utf8.RuneCountInString(label)+2)
	width := max(manpageWrapWidth-utf8.RuneCountInString(prefix), 20)
	lines := wrapExampleComment(strings.Split(strings.TrimSpace(value), "\n"), width)
	for i, line := range lines {
		if i == 0 {
			out.WriteString(prefix)
		} else {
			out.WriteString(continuation)
		}
		out.WriteString(line)
		out.WriteByte('\n')
	}
}

func streamIsNone(value string) bool {
	value = strings.TrimSpace(value)
	return value == "" || strings.EqualFold(value, "none")
}

func publicCommands(root *cobra.Command) []*cobra.Command {
	var commands []*cobra.Command
	var walk func(*cobra.Command)
	walk = func(parent *cobra.Command) {
		for _, child := range publicCommandChildren(parent) {
			commands = append(commands, child)
			walk(child)
		}
	}
	walk(root)
	return commands
}

func publicCommandChildren(parent *cobra.Command) []*cobra.Command {
	children := append([]*cobra.Command(nil), parent.Commands()...)
	sort.Slice(children, func(i, j int) bool {
		return children[i].Name() < children[j].Name()
	})
	visible := children[:0]
	for _, child := range children {
		if !child.Hidden {
			visible = append(visible, child)
		}
	}
	return visible
}

func commandHTMLAnchor(root, command *cobra.Command) string {
	path := strings.TrimPrefix(command.CommandPath(), root.CommandPath()+" ")
	return "COMMAND_" + strings.ReplaceAll(path, " ", "-")
}

func splitManpageRootDescription(description string) (overview, resolution, outputBehavior string) {
	paragraphs := strings.Split(strings.TrimSpace(description), "\n\n")
	marker := -1
	for i, paragraph := range paragraphs {
		if strings.TrimSpace(paragraph) == "Precedence rules:" {
			marker = i
			break
		}
	}
	if marker < 0 {
		return strings.TrimSpace(description), "None.", "None."
	}

	overview = strings.Join(paragraphs[:marker], "\n\n")
	reference := paragraphs[marker+1:]
	if len(reference) == 0 {
		return overview, "None.", "None."
	}
	last := strings.TrimSpace(reference[len(reference)-1])
	if strings.HasPrefix(last, "Table output for human readability") {
		outputBehavior = last
		reference = reference[:len(reference)-1]
	}
	resolution = strings.Join(reference, "\n\n")
	if strings.TrimSpace(resolution) == "" {
		resolution = "None."
	}
	if strings.TrimSpace(outputBehavior) == "" {
		outputBehavior = "None."
	}
	return overview, resolution, outputBehavior
}

func rootGlobalFlags(root *cobra.Command) []*pflag.Flag {
	var flags []*pflag.Flag
	root.PersistentFlags().VisitAll(func(flag *pflag.Flag) {
		if manpageFlagVisible(flag) {
			flags = append(flags, flag)
		}
	})
	sort.Slice(flags, func(i, j int) bool { return flags[i].Name < flags[j].Name })
	return flags
}

func commandLocalFlags(command *cobra.Command) []*pflag.Flag {
	seen := map[string]bool{}
	var flags []*pflag.Flag
	add := func(set *pflag.FlagSet) {
		set.VisitAll(func(flag *pflag.Flag) {
			if !seen[flag.Name] && manpageFlagVisible(flag) {
				seen[flag.Name] = true
				flags = append(flags, flag)
			}
		})
	}
	add(command.LocalNonPersistentFlags())
	add(command.PersistentFlags())
	sort.Slice(flags, func(i, j int) bool { return flags[i].Name < flags[j].Name })
	return flags
}

func commandInheritedFlags(root, command *cobra.Command) []*pflag.Flag {
	seen := map[string]bool{}
	var flags []*pflag.Flag
	for parent := command.Parent(); parent != nil && parent != root; parent = parent.Parent() {
		parent.PersistentFlags().VisitAll(func(flag *pflag.Flag) {
			if !seen[flag.Name] && manpageFlagVisible(flag) {
				seen[flag.Name] = true
				flags = append(flags, flag)
			}
		})
	}
	sort.Slice(flags, func(i, j int) bool { return flags[i].Name < flags[j].Name })
	return flags
}

func manpageFlagVisible(flag *pflag.Flag) bool {
	return flag != nil && !flag.Hidden && flag.Deprecated == "" &&
		flag.Name != "help" && flag.Name != "version"
}

func writeFlagList(b *strings.Builder, command *cobra.Command, flags []*pflag.Flag, effective bool) {
	if len(flags) == 0 {
		writeWrapped(b, "None.")
		return
	}
	for _, flag := range flags {
		usage := flag.Usage
		if effective {
			if resolved, ok := commandGlobalFlagUsage(command, flag.Name); ok {
				usage = resolved
			} else if resolved, ok := commandOwnedFlagUsage(command, flag.Name); ok {
				usage = resolved
			}
		}
		writeWrapped(b, fmt.Sprintf("* %s - %s%s", flagSignature(flag), usage, flagDefault(flag)))
	}
}

func flagSignature(flag *pflag.Flag) string {
	parts := make([]string, 0, 2)
	if flag.Shorthand != "" && flag.ShorthandDeprecated == "" {
		parts = append(parts, "**-"+flag.Shorthand+"**")
	}
	long := "**--" + flag.Name + "**"
	if flag.NoOptDefVal == "" {
		long += "=*" + flagValueName(flag) + "*"
	}
	parts = append(parts, long)
	return strings.Join(parts, ", ")
}

func flagValueName(flag *pflag.Flag) string {
	switch flag.Value.Type() {
	case "string":
		return "value"
	case "duration":
		return "duration"
	case "int", "int64":
		return "number"
	default:
		return flag.Value.Type()
	}
}

func flagDefault(flag *pflag.Flag) string {
	if flag.DefValue == "" || flag.DefValue == "false" || flag.DefValue == "0" {
		return ""
	}
	return " (default " + strconv.Quote(flag.DefValue) + ")"
}

func writeEnvironment(b *strings.Builder) {
	entries := [][2]string{
		{"AXILIO_API_KEY", "API credential. Precedence: --api-key, AXILIO_API_KEY, saved config key, then the saved OAuth session."},
		{"AXILIO_BASE_URL", "API host. Precedence: --base-url, AXILIO_BASE_URL, saved base-url, then https://api.axilio.ai."},
		{"AXILIO_ORG", "OAuth organization slug or ID. Precedence: --org, AXILIO_ORG, saved active organization, then the OAuth session default. API keys remain scoped to the organization that created them."},
		{"AXILIO_SESSION", "Selects a local phone lease. Precedence: --session, AXILIO_SESSION, the sole active lease, saved current-session pointer, then an ambiguity error."},
		{"AXILIO_DASHBOARD_URL", "Overrides the dashboard host used for browser OAuth. It does not change the API host."},
		{"XDG_CONFIG_HOME", "Moves config, OAuth fallback, lease, and update-check state from $HOME/.config to $XDG_CONFIG_HOME."},
		{"NO_COLOR", "Set to 1 to disable color in Fang help; text decoration may remain. For terminal output it takes precedence over CLICOLOR and CLICOLOR_FORCE. With the pinned Fang color-profile dependency, CLICOLOR_FORCE can still force ANSI for non-terminal output."},
		{"CLICOLOR", "Set to 1 to request ANSI color in Fang help when output is a terminal and TERM is not dumb."},
		{"CLICOLOR_FORCE", "Set to 1 to force ANSI color in Fang help for non-terminal or dumb-terminal output. For non-terminal output in the pinned dependency, this also overrides NO_COLOR."},
		{"TERM", "TERM=dumb disables Fang help styling unless CLICOLOR_FORCE=1 is set."},
	}
	for _, entry := range entries {
		writeWrapped(b, fmt.Sprintf("**%s**\n: %s", entry[0], entry[1]))
	}
}

func writeExitStatuses(b *strings.Builder) {
	entries := []struct {
		code exit.Code
		name string
		desc string
	}{
		{exit.OK, "ok", "Success."},
		{exit.Err, "error", "Generic or unclassified failure, including local validation or lookup errors that were not explicitly coded."},
		{exit.Usage, "usage", "Explicit usage errors, recognized Cobra parse or arity errors, mobile invalid_args, or HTTP 400 or 422."},
		{exit.Auth, "auth", "Explicit authentication errors, mobile unauthorized, or HTTP 401 or 403."},
		{exit.NotFound, "not-found", "Mobile element_not_found or no_allocation, or HTTP 404."},
		{exit.Timeout, "timeout", "Explicit or mobile timeout, context deadline, or HTTP 408."},
		{exit.Unavailable, "unavailable", "Mobile connection, not_connected, or device_offline, or HTTP 429 or 5xx."},
		{exit.Canceled, "canceled", "Explicit, mobile, or context cancellation."},
	}
	for _, entry := range entries {
		writeWrapped(b, fmt.Sprintf("**%d (%s)**\n: %s", entry.code, entry.name, entry.desc))
	}
}

func writeFiles(b *strings.Builder) {
	entries := [][2]string{
		{"${XDG_CONFIG_HOME:-$HOME/.config}/axilio/config.json", "Saved API key, API host, and active organization. A new file requests mode 0600; umask may make it more restrictive. Overwriting preserves its mode."},
		{"OS keychain: service axilio-cli, account oauth-tokens", "Preferred storage for browser-OAuth access and refresh tokens."},
		{"${XDG_CONFIG_HOME:-$HOME/.config}/axilio/oauth.json", "OAuth-token fallback when the OS keychain is unavailable. A new file requests mode 0600; umask may make it more restrictive. Overwriting preserves its mode."},
		{"${XDG_CONFIG_HOME:-$HOME/.config}/axilio/sessions/<session-id>.json", "Mode-0600 local lease record containing the session's control URL."},
		{"${XDG_CONFIG_HOME:-$HOME/.config}/axilio/current-session", "Pointer to the last-started local session. A new file requests mode 0600; umask may make it more restrictive. Overwriting preserves its mode."},
		{"${XDG_CONFIG_HOME:-$HOME/.config}/axilio/update-check.json", "Daily release-check cache. Failures reading or writing it do not fail commands."},
		{"./screenshot.png", "Default output of `axilio phone screenshot`; --out selects another path and existing contents are overwritten."},
		{".claude/skills/axilio/SKILL.md", "Agent instructions written by `axilio init --agent claude`."},
		{"AGENTS.md", "Shared file updated inside Axilio markers by `axilio init --agent codex`."},
		{".cursor/rules/axilio.mdc", "Agent instructions written by `axilio init --agent cursor`."},
	}
	for _, entry := range entries {
		writeWrapped(b, fmt.Sprintf("**%s**\n: %s", entry[0], entry[1]))
	}
}

func writeDescription(b *strings.Builder, description string) {
	description = strings.ReplaceAll(description, "\r\n", "\n")
	description = strings.TrimSpace(description)
	if description == "" {
		return
	}
	for _, paragraph := range splitParagraphs(description) {
		lines := strings.Split(paragraph, "\n")
		for len(lines) > 0 {
			literal := startsIndented(lines[0])
			end := 1
			for end < len(lines) && startsIndented(lines[end]) == literal {
				end++
			}
			group := lines[:end]
			if literal {
				for i := range group {
					group[i] = strings.TrimPrefix(strings.TrimPrefix(group[i], "\t"), "  ")
				}
				writeLiteral(b, strings.Join(group, "\n"))
			} else {
				writeWrapped(b, strings.Join(group, " "))
			}
			lines = lines[end:]
		}
	}
}

func splitParagraphs(text string) []string {
	var paragraphs []string
	start := 0
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) != "" {
			continue
		}
		if paragraph := strings.TrimSpace(strings.Join(lines[start:i], "\n")); paragraph != "" {
			paragraphs = append(paragraphs, paragraph)
		}
		start = i + 1
	}
	if paragraph := strings.TrimSpace(strings.Join(lines[start:], "\n")); paragraph != "" {
		paragraphs = append(paragraphs, paragraph)
	}
	return paragraphs
}

func startsIndented(line string) bool {
	return strings.HasPrefix(line, "  ") || strings.HasPrefix(line, "\t")
}

func writeWrapped(b *strings.Builder, text string) {
	text = strings.Join(strings.Fields(text), " ")
	text = escapeMarkdownAngles(text)
	if text == "" {
		return
	}
	lineWidth := 0
	for _, word := range strings.Fields(text) {
		width := utf8.RuneCountInString(word)
		if lineWidth > 0 && lineWidth+1+width > manpageWrapWidth {
			b.WriteByte('\n')
			lineWidth = 0
		}
		if lineWidth > 0 {
			b.WriteByte(' ')
			lineWidth++
		}
		b.WriteString(word)
		lineWidth += width
	}
	b.WriteString("\n\n")
}

func writeLiteral(b *strings.Builder, text string) {
	writeFencedLiteral(b, text, "")
}

func writeTerminalLiteral(b *strings.Builder, text string) {
	writeFencedLiteral(b, text, "console")
}

func writeFencedLiteral(b *strings.Builder, text, language string) {
	text = strings.Trim(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if text == "" {
		return
	}
	b.WriteString("```")
	b.WriteString(language)
	b.WriteByte('\n')
	b.WriteString(text)
	b.WriteString("\n```\n\n")
}

func escapeMarkdownAngles(text string) string {
	// Backslash escapes keep placeholders and shell redirections out of
	// Blackfriday's HTML/autolink parser without leaking HTML entities into
	// the roff output.
	return strings.NewReplacer("<", `\<`, ">", `\>`).Replace(text)
}

func deindent(text string) string {
	lines := strings.Split(strings.Trim(text, "\n"), "\n")
	for i := range lines {
		lines[i] = strings.TrimPrefix(lines[i], "  ")
	}
	return strings.Join(lines, "\n")
}

func firstNonempty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
