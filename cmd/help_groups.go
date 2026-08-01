package cmd

import (
	"bytes"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type helpFlagGroup struct {
	title string
	names map[string]bool
}

// groupFlagsByOwner retains the configured help renderer while separating
// flags according to the command that defines them. Cobra exposes flag
// ownership, but Fang renders the merged flag set as one section.
func groupFlagsByOwner(command *cobra.Command) {
	command.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		renderer := cmd.Root().HelpFunc()
		if cmd == cmd.Root() {
			renderer(cmd, args)
			return
		}

		groups, flags := ownedFlagGroups(cmd)
		if len(groups) < 2 || len(flags) == 0 {
			renderer(cmd, args)
			return
		}

		output := cmd.OutOrStdout()
		originalHidden := make(map[*pflag.Flag]bool, len(flags))
		for _, flag := range flags {
			originalHidden[flag] = flag.Hidden
		}
		defer func() {
			cmd.SetOut(output)
			for flag, hidden := range originalHidden {
				flag.Hidden = hidden
			}
		}()

		full := captureHelp(cmd, args, renderer, output)
		preamble, _, ok := splitFlagSection(full)
		if !ok {
			_, _ = fmt.Fprint(output, full)
			return
		}

		var grouped strings.Builder
		grouped.WriteString(preamble)
		for _, group := range groups {
			for _, flag := range flags {
				flag.Hidden = originalHidden[flag] || !group.names[flag.Name]
			}
			_, section, found := splitFlagSection(captureHelp(cmd, args, renderer, output))
			if !found {
				continue
			}
			grouped.WriteString(strings.Replace(section, "FLAGS", group.title, 1))
		}
		_, _ = fmt.Fprint(output, grouped.String())
	})
}

func ownedFlagGroups(cmd *cobra.Command) ([]helpFlagGroup, []*pflag.Flag) {
	cmd.InitDefaultHelpFlag()
	available := map[string]*pflag.Flag{}
	cmd.Flags().VisitAll(func(flag *pflag.Flag) {
		if !flag.Hidden {
			available[flag.Name] = flag
		}
	})

	seen := map[string]bool{}
	var groups []helpFlagGroup
	addGroup := func(title string, sets ...*pflag.FlagSet) {
		names := map[string]bool{}
		for _, set := range sets {
			set.VisitAll(func(flag *pflag.Flag) {
				if _, ok := available[flag.Name]; ok && !seen[flag.Name] {
					names[flag.Name] = true
					seen[flag.Name] = true
				}
			})
		}
		if len(names) > 0 {
			groups = append(groups, helpFlagGroup{title: title, names: names})
		}
	}

	addGroup(strings.ToUpper(cmd.Name())+" FLAGS", cmd.LocalNonPersistentFlags(), cmd.PersistentFlags())
	for parent := cmd.Parent(); parent != nil && parent != cmd.Root(); parent = parent.Parent() {
		addGroup(strings.ToUpper(parent.Name())+" FLAGS", parent.PersistentFlags())
	}
	addGroup("GLOBAL FLAGS", cmd.Root().PersistentFlags())

	// Keep generated or renderer-added flags visible even if Cobra does not
	// expose an owning flag set for them.
	if len(seen) < len(available) {
		if len(groups) == 0 {
			groups = append(groups, helpFlagGroup{
				title: strings.ToUpper(cmd.Name()) + " FLAGS",
				names: map[string]bool{},
			})
		}
		for name := range available {
			if !seen[name] {
				groups[0].names[name] = true
			}
		}
	}

	flags := make([]*pflag.Flag, 0, len(available))
	for _, flag := range available {
		flags = append(flags, flag)
	}
	return groups, flags
}

func captureHelp(
	cmd *cobra.Command,
	args []string,
	renderer func(*cobra.Command, []string),
	output io.Writer,
) string {
	var buffer bytes.Buffer
	cmd.SetOut(helpCaptureWriter(&buffer, output))
	renderer(cmd, args)
	cmd.SetOut(output)
	return buffer.String()
}

// helpCaptureWriter makes an in-memory capture look like the destination TTY
// so the renderer keeps the same color profile it would use without grouping.
func helpCaptureWriter(buffer *bytes.Buffer, output io.Writer) io.Writer {
	if file, ok := output.(interface{ Fd() uintptr }); ok {
		return &terminalHelpCapture{Buffer: buffer, fd: file.Fd()}
	}
	return buffer
}

type terminalHelpCapture struct {
	*bytes.Buffer
	fd uintptr
}

var _ interface {
	io.ReadWriteCloser
	Fd() uintptr
} = (*terminalHelpCapture)(nil)

func (capture *terminalHelpCapture) Fd() uintptr { return capture.fd }

func (capture *terminalHelpCapture) Close() error { return nil }

func splitFlagSection(rendered string) (preamble, flags string, ok bool) {
	lineStart := 0
	for _, line := range strings.SplitAfter(rendered, "\n") {
		plain := helpANSIPattern.ReplaceAllString(line, "")
		if strings.TrimSpace(plain) == "FLAGS" {
			return rendered[:lineStart], rendered[lineStart:], true
		}
		lineStart += len(line)
	}
	return rendered, "", false
}

var helpANSIPattern = regexp.MustCompile(`\x1b\[[0-9;:?]*[ -/]*[@-~]`)
