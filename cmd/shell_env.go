package cmd

import (
	"fmt"
	"os"
	"runtime"

	"github.com/axilioai/cli/internal/exit"
)

// Shell dialects for the environment assignment that `sessions start --export`
// prints and that the "pin it to this shell" hint shows.
const (
	shellPOSIX      = "posix"
	shellPowerShell = "powershell"
	shellCmd        = "cmd"
)

const shellFlagHelp = "Assignment syntax for --export and the pin hint: posix, powershell, or cmd " +
	"(default posix; powershell on Windows unless SHELL is set, as Git Bash and MSYS2 do)"

// resolveShell returns the dialect for --shell. An empty flag picks the
// platform default.
func resolveShell(flag string) (string, error) {
	switch flag {
	case "":
		return defaultShell(runtime.GOOS, os.Getenv), nil
	case shellPOSIX, shellPowerShell, shellCmd:
		return flag, nil
	}
	return "", exit.Usagef("unsupported --shell %q; supported values: %s, %s, %s",
		flag, shellPOSIX, shellPowerShell, shellCmd)
}

// defaultShell is PowerShell on Windows, its default shell. Git Bash, MSYS2 and
// Cygwin set SHELL and expect POSIX syntax, so a set SHELL keeps POSIX there.
func defaultShell(goos string, getenv func(string) string) string {
	if goos == "windows" && getenv("SHELL") == "" {
		return shellPowerShell
	}
	return shellPOSIX
}

// envAssignment renders `name=value` in the given dialect. The POSIX form stays
// unquoted: it is the long-standing --export contract and values are IDs.
func envAssignment(shell, name, value string) string {
	switch shell {
	case shellPowerShell:
		return fmt.Sprintf("$env:%s = \"%s\"", name, value)
	case shellCmd:
		return fmt.Sprintf("set %s=%s", name, value)
	default:
		return fmt.Sprintf("export %s=%s", name, value)
	}
}
