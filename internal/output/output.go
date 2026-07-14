// Package output renders command results either as human tables or as clean
// JSON, and keeps the two disciplined: data (JSON, tables) goes to stdout, human
// chrome (notes, prompts) to stderr, and in JSON mode the chrome is suppressed
// so a pipe into jq never sees stray text.
package output

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/pterm/pterm"
)

// Printer carries the chosen output mode.
type Printer struct {
	JSON  bool
	Quiet bool
}

// New builds a Printer for the given --output value, --no-color, and --quiet.
func New(format string, noColor, quiet bool) *Printer {
	if noColor {
		pterm.DisableColor()
	}
	return &Printer{JSON: format == "json", Quiet: quiet}
}

// silent reports whether human chrome is suppressed (JSON or --quiet).
func (p *Printer) silent() bool { return p.JSON || p.Quiet }

// Emit prints v as indented JSON to stdout in JSON mode; otherwise it runs the
// table builder. The generated SDK response types marshal cleanly (omitempty
// drops nil pointers), so JSON stays faithful without leaking defaults.
func (p *Printer) Emit(v any, table func()) {
	if p.JSON {
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return
		}
		fmt.Println(string(b))
		return
	}
	table()
}

// Note prints human chrome to stderr, suppressed in JSON or --quiet mode.
func (p *Printer) Note(format string, a ...any) {
	if p.silent() {
		return
	}
	fmt.Fprintf(os.Stderr, format+"\n", a...)
}

// Confirm asks a yes/no question on stderr and reads the answer from stdin.
// When output is non-interactive (JSON or --quiet), it never prompts and
// returns false, so a destructive command declines rather than hanging — pass
// an explicit --yes to proceed in that mode.
func (p *Printer) Confirm(prompt string) bool {
	if p.silent() {
		return false
	}
	fmt.Fprintf(os.Stderr, "%s [y/N] ", prompt)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes"
}

// Table renders rows (rows[0] is the header) to stdout.
func Table(rows [][]string) {
	_ = pterm.DefaultTable.WithHasHeader().WithData(pterm.TableData(rows)).Render()
}

// KV renders a two-column property/value detail view to stdout.
func KV(pairs [][2]string) {
	rows := make([][]string, 0, len(pairs))
	for _, kv := range pairs {
		rows = append(rows, []string{kv[0], kv[1]})
	}
	_ = pterm.DefaultTable.WithData(pterm.TableData(rows)).Render()
}
