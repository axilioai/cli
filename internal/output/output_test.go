package output

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// Warn exists because every other stderr channel (Note/Success/Step) is gated on
// silent(), which is true in JSON mode — and agents drive with -o json. A warning
// muted for agents is a warning that never reaches the audience it was written
// for. These tests pin that exception, since it looks like an inconsistency and
// would be "tidied away" by someone who didn't know why it's there.

func TestWarnSurvivesJSONMode(t *testing.T) {
	p := New("json", true, false)
	if got := captureStderr(t, func() { p.Warn("brittle: %s", "coords") }); !strings.Contains(got, "brittle: coords") {
		t.Fatalf("Warn was suppressed in JSON mode; an agent driving with -o json would never "+
			"see it, which is the entire reason Warn exists. stderr=%q", got)
	}
}

// The contract is that *stdout* stays clean so a jq pipe never breaks. stderr was
// always ours, so warning there costs nothing.
func TestWarnWritesNothingToStdout(t *testing.T) {
	p := New("json", true, false)
	if got := captureStdout(t, func() { p.Warn("brittle") }); got != "" {
		t.Fatalf("Warn wrote %q to stdout; that would corrupt a `... -o json | jq` pipe", got)
	}
}

// --quiet means "no chrome at all" — an explicit request we honor.
func TestWarnIsSilencedByQuiet(t *testing.T) {
	p := New("json", true, true)
	if got := captureStderr(t, func() { p.Warn("brittle") }); got != "" {
		t.Fatalf("--quiet must silence Warn, got %q", got)
	}
}

// Note stays suppressed in JSON mode — Warn is the deliberate exception, not a
// new general rule.
func TestNoteRemainsSuppressedInJSONMode(t *testing.T) {
	p := New("json", true, false)
	if got := captureStderr(t, func() { p.Note("chrome") }); got != "" {
		t.Fatalf("Note must stay suppressed in JSON mode, got %q", got)
	}
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	return capture(t, &os.Stderr, fn)
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	return capture(t, &os.Stdout, fn)
}

func capture(t *testing.T, target **os.File, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := *target
	*target = w
	defer func() { *target = orig }()

	done := make(chan string)
	go func() {
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(r)
		done <- buf.String()
	}()

	fn()
	_ = w.Close()
	return <-done
}
