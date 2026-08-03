package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/axilioai/cli/internal/exit"
)

// execRootStreams captures the three process streams used by output.Printer.
// Stdin is always a pipe, which makes redirected-execution behavior
// deterministic even when the test binary itself was launched from a terminal.
func execRootStreams(t *testing.T, stdin string, args ...string) (string, string, error) {
	t.Helper()
	origIn, origOut, origErr := os.Stdin, os.Stdout, os.Stderr
	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(inW, stdin); err != nil {
		t.Fatal(err)
	}
	_ = inW.Close()
	os.Stdin, os.Stdout, os.Stderr = inR, outW, errW
	defer func() {
		os.Stdin, os.Stdout, os.Stderr = origIn, origOut, origErr
		_ = inR.Close()
		_ = outR.Close()
		_ = errR.Close()
	}()

	root := Root()
	root.SetArgs(args)
	execErr := root.Execute()
	_ = outW.Close()
	_ = errW.Close()
	var stdout, stderr bytes.Buffer
	_, _ = io.Copy(&stdout, outR)
	_, _ = io.Copy(&stderr, errR)
	return stdout.String(), stderr.String(), execErr
}

func TestConfigSetUsesStdout(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("AXILIO_BASE_URL", "")
	t.Setenv("AXILIO_API_KEY", "")
	stdout, stderr, err := execRootStreams(t, "", "config", "set", "base-url", "https://api.axilio.ai")
	if err != nil {
		t.Fatal(err)
	}
	if want := "Set base-url = https://api.axilio.ai in "; !strings.HasPrefix(stdout, want) {
		t.Fatalf("stdout = %q, want prefix %q", stdout, want)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
}

func TestQuietSuppressesAcknowledgmentsButPreservesData(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("AXILIO_BASE_URL", "")
	t.Setenv("AXILIO_API_KEY", "")
	stdout, stderr, err := execRootStreams(t, "", "--quiet", "config", "set", "base-url", "https://api.axilio.ai")
	if err != nil {
		t.Fatal(err)
	}
	if stdout != "" || stderr != "" {
		t.Fatalf("quiet action streams = stdout %q, stderr %q", stdout, stderr)
	}

	stdout, stderr, err = execRootStreams(t, "", "--quiet", "config")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "https://api.axilio.ai") {
		t.Fatalf("quiet data command lost its primary result: %q", stdout)
	}
	if stderr != "" {
		t.Fatalf("quiet data stderr = %q, want empty", stderr)
	}
}

func TestJSONIsOneDocumentWithNoOptionalHumanOutput(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("AXILIO_BASE_URL", "")
	t.Setenv("AXILIO_API_KEY", "")
	stdout, stderr, err := execRootStreams(t, "", "-o", "json", "config", "set", "base-url", "https://api.axilio.ai")
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	dec := json.NewDecoder(strings.NewReader(stdout))
	if err := dec.Decode(&result); err != nil {
		t.Fatalf("decode result: %v\n%s", err, stdout)
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		t.Fatalf("stdout contains more than one JSON value: %q", stdout)
	}
	if result["key"] != "base-url" || stderr != "" {
		t.Fatalf("result = %#v, stderr = %q", result, stderr)
	}
}

func TestRedirectedConfirmationNeverConsumesYes(t *testing.T) {
	stdout, stderr, err := execRootStreams(t, "yes\n", "runs", "cancel", "run_123")
	if err == nil || exit.Classify(err) != exit.Usage {
		t.Fatalf("error = %v, want usage error", err)
	}
	if stdout != "" || stderr != "" {
		t.Fatalf("redirected destructive command prompted: stdout %q, stderr %q", stdout, stderr)
	}
}

func TestSessionsExportRejectsJSONBeforeAllocation(t *testing.T) {
	stdout, stderr, err := execRootStreams(t, "", "-o", "json", "sessions", "start", "--export")
	if err == nil || exit.Classify(err) != exit.Usage {
		t.Fatalf("error = %v, want usage error", err)
	}
	if !strings.Contains(err.Error(), "--export cannot be combined") {
		t.Fatalf("error does not explain conflict: %v", err)
	}
	if stdout != "" || stderr != "" {
		t.Fatalf("invalid export wrote output: stdout %q, stderr %q", stdout, stderr)
	}
}
