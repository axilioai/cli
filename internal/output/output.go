// Package output renders command results either as human tables or as clean
// JSON, and keeps the two disciplined: data (JSON, tables) goes to stdout, human
// chrome (notes, prompts) to stderr, and in JSON mode the chrome is suppressed
// so a pipe into jq never sees stray text.
package output

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/pterm/pterm"
)

// Printer carries the chosen output mode.
type Printer struct {
	JSON bool
}

// New builds a Printer for the given --output value and --no-color.
func New(format string, noColor bool) *Printer {
	if noColor {
		pterm.DisableColor()
	}
	return &Printer{JSON: format == "json"}
}

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

// Note prints human chrome to stderr, suppressed in JSON mode.
func (p *Printer) Note(format string, a ...any) {
	if p.JSON {
		return
	}
	fmt.Fprintf(os.Stderr, format+"\n", a...)
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
