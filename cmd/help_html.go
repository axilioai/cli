package cmd

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/axilioai/cli/internal/exit"
	"github.com/spf13/cobra"
)

const htmlManualName = "axilio.1.html"

// configureHelpCommand extends Cobra's generated help command without adding
// another top-level command solely for documentation discovery.
func configureHelpCommand(root *cobra.Command) {
	root.InitDefaultHelpCmd()

	var help *cobra.Command
	for _, command := range root.Commands() {
		if command.Name() == "help" {
			help = command
			break
		}
	}
	if help == nil {
		panic("configure help command: Cobra did not create a help command")
	}

	help.Short = "Help about any command, or locate the local HTML manual."
	help.Long = `Show help for axilio or one of its commands.

Use --html without a command name to print a clickable file:// URL for the
installed, browser-friendly manual.`

	var html bool
	help.Flags().BoolVar(&html, "html", false, "Print the file:// URL for the installed HTML manual")
	originalRun := help.Run
	help.Run = nil
	help.RunE = func(command *cobra.Command, args []string) error {
		if !html {
			originalRun(command, args)
			return nil
		}
		if len(args) != 0 {
			return exit.Usagef("--html cannot be combined with a command name")
		}

		path, err := installedHTMLManualPath()
		if err != nil {
			return exit.With(exit.NotFound, err)
		}
		_, err = fmt.Fprintln(command.OutOrStdout(), htmlManualFileURL(path))
		return err
	}
}

func installedHTMLManualPath() (string, error) {
	var candidates []string
	if output, err := exec.Command("man", "-w", "axilio").Output(); err == nil {
		for _, line := range strings.Split(string(output), "\n") {
			roff := strings.TrimSpace(line)
			if roff == "" {
				continue
			}
			candidates = appendManualSibling(candidates, roff)
			if resolved, err := filepath.EvalSymlinks(roff); err == nil {
				candidates = appendManualSibling(candidates, resolved)
			}
		}
	}

	if executable, err := os.Executable(); err == nil {
		candidates = appendExecutableManualCandidates(candidates, executable)
		if resolved, err := filepath.EvalSymlinks(executable); err == nil {
			candidates = appendExecutableManualCandidates(candidates, resolved)
		}
	}
	if workingDirectory, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(workingDirectory, "man", htmlManualName))
	}

	seen := make(map[string]bool, len(candidates))
	for _, candidate := range candidates {
		candidate = filepath.Clean(candidate)
		if seen[candidate] {
			continue
		}
		seen[candidate] = true
		info, err := os.Stat(candidate)
		if err == nil && info.Mode().IsRegular() {
			absolute, err := filepath.Abs(candidate)
			if err != nil {
				return "", fmt.Errorf("resolve local HTML manual: %w", err)
			}
			return absolute, nil
		}
	}

	return "", fmt.Errorf("local HTML manual not found; reinstall with Homebrew or install.sh, or open man/%s from a release archive", htmlManualName)
}

func appendManualSibling(candidates []string, roff string) []string {
	return append(candidates, filepath.Join(filepath.Dir(roff), htmlManualName))
}

func appendExecutableManualCandidates(candidates []string, executable string) []string {
	directory := filepath.Dir(executable)
	candidates = append(candidates, filepath.Join(directory, "man", htmlManualName))
	switch filepath.Base(directory) {
	case "bin", "sbin":
		candidates = append(candidates,
			filepath.Join(filepath.Dir(directory), "share", "man", "man1", htmlManualName))
	}
	return candidates
}

func htmlManualFileURL(path string) string {
	path = filepath.ToSlash(path)
	if filepath.VolumeName(path) != "" && !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return (&url.URL{Scheme: "file", Path: path}).String()
}
