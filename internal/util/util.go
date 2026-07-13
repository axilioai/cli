// Package util holds small CLI helpers.
package util

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// FirstNonEmpty returns the first non-empty string (flag > env > config precedence).
func FirstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// OrDash renders an empty string as a dash for table cells.
func OrDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// Confirm asks a yes/no question on stderr and reads the answer from stdin.
func Confirm(prompt string) bool {
	fmt.Fprintf(os.Stderr, "%s [y/N] ", prompt)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes"
}
