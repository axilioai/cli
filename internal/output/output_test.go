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

func TestOutputClassRouting(t *testing.T) {
	type routedOutput struct {
		stdout string
		stderr string
	}
	tests := []struct {
		name               string
		write              func(*Printer)
		human, quiet, json routedOutput
	}{
		{name: "result", write: func(p *Printer) { p.Result("data") }, human: routedOutput{stdout: "data\n"}, quiet: routedOutput{stdout: "data\n"}},
		{name: "ack", write: func(p *Printer) { p.Ack("ack") }, human: routedOutput{stdout: "ack\n"}},
		{name: "success", write: func(p *Printer) { p.Success("success") }, human: routedOutput{stdout: "✓ success\n"}},
		{name: "note", write: func(p *Printer) { p.Note("note") }, human: routedOutput{stderr: "note\n"}},
		{name: "step", write: func(p *Printer) { p.Step("step") }, human: routedOutput{stderr: "→ step\n"}},
		{name: "warning", write: func(p *Printer) { p.Warn("warning") }, human: routedOutput{stderr: "warning: warning\n"}, quiet: routedOutput{stderr: "warning: warning\n"}, json: routedOutput{stderr: "warning: warning\n"}},
		{name: "prompt", write: func(p *Printer) { p.Prompt("prompt") }, human: routedOutput{stderr: "prompt"}},
	}

	for _, tc := range tests {
		for _, mode := range []struct {
			name   string
			format string
			quiet  bool
			want   routedOutput
		}{
			{name: "human", format: "table", want: tc.human},
			{name: "quiet", format: "table", quiet: true, want: tc.quiet},
			{name: "json", format: "json", want: tc.json},
		} {
			t.Run(tc.name+"/"+mode.name, func(t *testing.T) {
				p, stdout, stderr := testPrinter(mode.format, mode.quiet, true, "")
				tc.write(p)
				if got := stdout.String(); got != mode.want.stdout {
					t.Errorf("stdout = %q, want %q", got, mode.want.stdout)
				}
				if got := stderr.String(); got != mode.want.stderr {
					t.Errorf("stderr = %q, want %q", got, mode.want.stderr)
				}
			})
		}
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
