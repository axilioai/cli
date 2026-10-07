package cmd

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/axilioai/cli/internal/exit"
)

func TestDefaultShell(t *testing.T) {
	tests := []struct {
		name  string
		goos  string
		shell string
		want  string
	}{
		{name: "linux", goos: "linux", want: shellPOSIX},
		{name: "darwin", goos: "darwin", shell: "/bin/zsh", want: shellPOSIX},
		{name: "windows powershell or cmd", goos: "windows", want: shellPowerShell},
		{name: "windows git bash", goos: "windows", shell: "/usr/bin/bash", want: shellPOSIX},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			getenv := func(key string) string {
				if key == "SHELL" {
					return tt.shell
				}
				return ""
			}
			if got := defaultShell(tt.goos, getenv); got != tt.want {
				t.Errorf("defaultShell(%q, SHELL=%q) = %q, want %q", tt.goos, tt.shell, got, tt.want)
			}
		})
	}
}

func TestEnvAssignment(t *testing.T) {
	tests := []struct {
		shell string
		want  string
	}{
		{shell: shellPOSIX, want: "export AXILIO_SESSION=sess_1"},
		{shell: shellPowerShell, want: `$env:AXILIO_SESSION = "sess_1"`},
		{shell: shellCmd, want: "set AXILIO_SESSION=sess_1"},
	}
	for _, tt := range tests {
		if got := envAssignment(tt.shell, "AXILIO_SESSION", "sess_1"); got != tt.want {
			t.Errorf("envAssignment(%q) = %q, want %q", tt.shell, got, tt.want)
		}
	}
}

func TestSessionsStartExportShellSyntax(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, ":allocate") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"phone_id":"p1","session_id":"s1","workflow_started_at":"2026-08-03T00:00:00Z"}`)
	}))
	t.Cleanup(srv.Close)

	for shell, want := range map[string]string{
		shellPOSIX:      "export AXILIO_SESSION=s1\n",
		shellPowerShell: "$env:AXILIO_SESSION = \"s1\"\n",
		shellCmd:        "set AXILIO_SESSION=s1\n",
	} {
		out, err := run(t, srv, "sessions", "start", "--export", "--shell", shell)
		if err != nil {
			t.Fatalf("sessions start --export --shell %s: %v", shell, err)
		}
		if out != want {
			t.Errorf("--shell %s stdout = %q, want %q", shell, out, want)
		}
	}
}

func TestSessionsStartRejectsUnknownShellBeforeAllocation(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("AXILIO_API_KEY", "")
	t.Setenv("AXILIO_BASE_URL", "")
	stdout, stderr, err := execRootStreams(t, "", "sessions", "start", "--export", "--shell", "fish")
	if got := exit.Classify(err); got != exit.Usage {
		t.Fatalf("exit class = %d, want %d (err %v)", got, exit.Usage, err)
	}
	if stdout != "" || stderr != "" {
		t.Fatalf("invalid --shell wrote output: stdout %q, stderr %q", stdout, stderr)
	}
}
