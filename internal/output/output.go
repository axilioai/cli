// Package output renders command results while preserving a stable process
// contract: primary results use stdout; notes, progress, prompts, warnings, and
// errors use stderr. JSON success is exactly one document on stdout.
package output

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/pterm/pterm"
)

// Streams is the process boundary used by Printer. Production uses the real
// process streams; tests and embedded callers can inject isolated buffers.
type Streams struct {
	Stdout   io.Writer
	Stderr   io.Writer
	Stdin    io.Reader
	StdinTTY bool
}

// Printer carries the output mode and all process I/O used by a command.
type Printer struct {
	JSON  bool
	Quiet bool

	stdout   io.Writer
	stderr   io.Writer
	stdin    io.Reader
	stdinTTY bool
	err      error
}

// NewWithStreams builds a Printer with an explicit I/O boundary.
func NewWithStreams(format string, noColor, quiet bool, streams Streams) *Printer {
	if noColor {
		pterm.DisableColor()
	}
	if streams.Stdout == nil {
		streams.Stdout = io.Discard
	}
	if streams.Stderr == nil {
		streams.Stderr = io.Discard
	}
	if streams.Stdin == nil {
		streams.Stdin = strings.NewReader("")
	}
	return &Printer{
		JSON:     format == "json",
		Quiet:    quiet,
		stdout:   streams.Stdout,
		stderr:   streams.Stderr,
		stdin:    streams.Stdin,
		stdinTTY: streams.StdinTTY,
	}
}

// silentHuman reports whether optional human presentation is suppressed.
func (p *Printer) silentHuman() bool { return p.JSON || p.Quiet }

// Emit writes v as one indented JSON document in JSON mode; otherwise it runs
// the human renderer. Encoding and rendering errors propagate to the command.
func (p *Printer) Emit(v any, human func()) error {
	if p.err != nil {
		return p.err
	}
	if p.JSON {
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(p.stdout, string(b))
		return err
	}
	if human != nil {
		human()
	}
	return p.err
}

// JSONLine writes v as one compact JSON document on its own stdout line, and
// does nothing outside JSON mode. It exists for streaming commands (watch),
// whose JSON contract is newline-delimited JSON rather than Emit's single
// document; each such command documents that in its long help.
func (p *Printer) JSONLine(v any) error {
	if p.err != nil {
		return p.err
	}
	if !p.JSON {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(p.stdout, string(b))
	return err
}

// Result prints primary human result data to stdout. Unlike Ack, Result is not
// suppressed by --quiet. JSON callers should represent the value through Emit.
func (p *Printer) Result(format string, a ...any) {
	if p.JSON {
		return
	}
	p.write(p.stdout, format+"\n", a...)
}

// Ack prints a primary human success acknowledgment to stdout. It is replaced
// by structured output in JSON mode and suppressed by --quiet.
func (p *Printer) Ack(format string, a ...any) {
	if p.silentHuman() {
		return
	}
	p.write(p.stdout, format+"\n", a...)
}

// Success is a styled primary acknowledgment. It follows the same stream and
// mode rules as Ack.
func (p *Printer) Success(format string, a ...any) {
	if p.silentHuman() {
		return
	}
	p.write(p.stdout, "%s %s\n", pterm.Green("✓"), fmt.Sprintf(format, a...))
}

// Note prints supplemental human guidance to stderr. JSON and quiet suppress
// notes because they are not required to understand success or failure.
func (p *Printer) Note(format string, a ...any) {
	if p.silentHuman() {
		return
	}
	p.write(p.stderr, format+"\n", a...)
}

// Step prints line-oriented progress to stderr. JSON and quiet suppress it.
func (p *Printer) Step(format string, a ...any) {
	if p.silentHuman() {
		return
	}
	p.write(p.stderr, "%s %s\n", pterm.Gray("→"), fmt.Sprintf(format, a...))
}

// Warn prints actionable degradation to stderr in every mode, including JSON
// and quiet. Warnings never contaminate a successful result on stdout.
func (p *Printer) Warn(format string, a ...any) {
	p.write(p.stderr, "%s %s\n", pterm.Yellow("warning:"), fmt.Sprintf(format, a...))
}

// Prompt writes an interactive prompt fragment to stderr without adding a
// newline. It is available only for human TTY execution.
func (p *Printer) Prompt(format string, a ...any) {
	if p.silentHuman() || !p.stdinTTY {
		return
	}
	p.write(p.stderr, format, a...)
}

// Confirm asks a yes/no question on stderr and reads from stdin only when stdin
// is a TTY. JSON, quiet, and redirected execution never prompt; destructive
// commands must use their explicit --yes flag in those modes.
func (p *Printer) Confirm(prompt string) bool {
	if p.silentHuman() || !p.stdinTTY {
		return false
	}
	p.write(p.stderr, "%s [y/N] ", prompt)
	if p.err != nil {
		return false
	}
	line, err := bufio.NewReader(p.stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		p.err = err
		return false
	}
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes"
}

// Table renders rows (rows[0] is the header) to the configured stdout.
func (p *Printer) Table(rows [][]string) {
	if p.err != nil || p.JSON {
		return
	}
	p.err = pterm.DefaultTable.WithWriter(p.stdout).WithHasHeader().WithData(pterm.TableData(rows)).Render()
}

// KV renders a two-column property/value detail view to configured stdout.
func (p *Printer) KV(pairs [][2]string) {
	rows := make([][]string, 0, len(pairs))
	for _, kv := range pairs {
		rows = append(rows, []string{kv[0], kv[1]})
	}
	if p.err != nil || p.JSON {
		return
	}
	p.err = pterm.DefaultTable.WithWriter(p.stdout).WithData(pterm.TableData(rows)).Render()
}

// Err returns the first input or output error observed by the printer.
func (p *Printer) Err() error { return p.err }

func (p *Printer) write(w io.Writer, format string, a ...any) {
	if p.err != nil {
		return
	}
	_, p.err = fmt.Fprintf(w, format, a...)
}
