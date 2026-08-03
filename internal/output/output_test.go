package output

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func testPrinter(format string, quiet, tty bool, stdin string) (*Printer, *bytes.Buffer, *bytes.Buffer) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	return NewWithStreams(format, true, quiet, Streams{
		Stdout:   stdout,
		Stderr:   stderr,
		Stdin:    strings.NewReader(stdin),
		StdinTTY: tty,
	}), stdout, stderr
}

func TestHumanOutputClasses(t *testing.T) {
	p, stdout, stderr := testPrinter("table", false, true, "")
	p.Result("data")
	p.Ack("ack")
	p.Success("success")
	p.Note("note")
	p.Step("step")
	p.Warn("warning")
	p.Prompt("prompt")

	if got := stdout.String(); !strings.Contains(got, "data\nack\n") || !strings.Contains(got, "✓ success\n") {
		t.Fatalf("stdout = %q", got)
	}
	if got := stderr.String(); !strings.Contains(got, "note\n") || !strings.Contains(got, "→ step\n") || !strings.Contains(got, "warning: warning\n") || !strings.Contains(got, "prompt") {
		t.Fatalf("stderr = %q", got)
	}
}

func TestQuietPreservesDataWarningsAndErrors(t *testing.T) {
	p, stdout, stderr := testPrinter("table", true, false, "")
	p.Result("data")
	p.Ack("ack")
	p.Success("success")
	p.Note("note")
	p.Step("step")
	p.Warn("warning")
	p.Prompt("prompt")

	if got := stdout.String(); got != "data\n" {
		t.Fatalf("stdout = %q", got)
	}
	if got := stderr.String(); got != "warning: warning\n" {
		t.Fatalf("stderr = %q", got)
	}
}

func TestJSONEmitsOneDocumentAndSuppressesHumanRenderer(t *testing.T) {
	p, stdout, stderr := testPrinter("json", false, false, "")
	humanCalled := false
	if err := p.Emit(map[string]any{"ok": true}, func() {
		humanCalled = true
		p.Ack("ack")
	}); err != nil {
		t.Fatal(err)
	}
	if humanCalled {
		t.Fatal("human renderer ran in JSON mode")
	}
	if got := stdout.String(); got != "{\n  \"ok\": true\n}\n" {
		t.Fatalf("stdout = %q", got)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestJSONWarningStaysOnStderr(t *testing.T) {
	p, stdout, stderr := testPrinter("json", true, false, "")
	p.Warn("partial result")
	if err := p.Emit(map[string]string{"status": "ok"}, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(stdout.String(), "{\n") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if got := stderr.String(); got != "warning: partial result\n" {
		t.Fatalf("stderr = %q", got)
	}
}

func TestEmitReturnsMarshalAndWriteErrors(t *testing.T) {
	p, stdout, _ := testPrinter("json", false, false, "")
	if err := p.Emit(make(chan int), nil); err == nil {
		t.Fatal("marshal error = nil")
	}
	if stdout.Len() != 0 {
		t.Fatalf("marshal failure wrote stdout: %q", stdout.String())
	}

	want := errors.New("write failed")
	p = NewWithStreams("json", true, false, Streams{Stdout: errorWriter{want}, Stderr: &bytes.Buffer{}})
	if err := p.Emit(map[string]bool{"ok": true}, nil); !errors.Is(err, want) {
		t.Fatalf("write error = %v, want %v", err, want)
	}
}

func TestConfirmRequiresTTYAndInteractiveMode(t *testing.T) {
	for _, tc := range []struct {
		name   string
		format string
		quiet  bool
		tty    bool
		stdin  string
		want   bool
		prompt bool
	}{
		{name: "tty yes", format: "table", tty: true, stdin: "yes\n", want: true, prompt: true},
		{name: "tty no", format: "table", tty: true, stdin: "no\n", want: false, prompt: true},
		{name: "redirected", format: "table", tty: false, stdin: "yes\n", want: false},
		{name: "quiet", format: "table", quiet: true, tty: true, stdin: "yes\n", want: false},
		{name: "json", format: "json", tty: true, stdin: "yes\n", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, _, stderr := testPrinter(tc.format, tc.quiet, tc.tty, tc.stdin)
			if got := p.Confirm("Proceed?"); got != tc.want {
				t.Fatalf("Confirm() = %v, want %v", got, tc.want)
			}
			if got := stderr.Len() > 0; got != tc.prompt {
				t.Fatalf("prompt written = %v, want %v (%q)", got, tc.prompt, stderr.String())
			}
		})
	}
}

func TestTableAndKVUseInjectedStdout(t *testing.T) {
	p, stdout, _ := testPrinter("table", false, false, "")
	if err := p.Emit(nil, func() {
		p.Table([][]string{{"NAME", "VALUE"}, {"one", "two"}})
		p.KV([][2]string{{"Three", "four"}})
	}); err != nil {
		t.Fatal(err)
	}
	got := stdout.String()
	for _, want := range []string{"NAME", "VALUE", "one", "two", "Three", "four"} {
		if !strings.Contains(got, want) {
			t.Fatalf("stdout does not contain %q:\n%s", want, got)
		}
	}
}

type errorWriter struct{ err error }

func (w errorWriter) Write([]byte) (int, error) { return 0, w.err }
