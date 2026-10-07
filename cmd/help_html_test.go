package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/axilioai/cli/internal/exit"
)

func TestHelpHTMLPrintsInstalledManualFileURL(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stubs man(1) with a POSIX shell script; Windows has no man")
	}
	temp := t.TempDir()
	stagedManualDirectory := filepath.Join(temp, "Caskroom", "manual files")
	linkedManualDirectory := filepath.Join(temp, "share", "man", "man1")
	toolDirectory := filepath.Join(temp, "tools")
	for _, directory := range []string{stagedManualDirectory, linkedManualDirectory, toolDirectory} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	stagedRoff := filepath.Join(stagedManualDirectory, "axilio.1")
	stagedHTML := filepath.Join(stagedManualDirectory, htmlManualName)
	linkedRoff := filepath.Join(linkedManualDirectory, "axilio.1")
	if err := os.WriteFile(stagedRoff, []byte(".TH AXILIO 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stagedHTML, []byte("<!doctype html>\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(stagedRoff, linkedRoff); err != nil {
		t.Fatal(err)
	}

	manStub := filepath.Join(toolDirectory, "man")
	stub := "#!/bin/sh\nprintf '%s\\n' \"$AXILIO_TEST_MANPAGE\"\n"
	if err := os.WriteFile(manStub, []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", toolDirectory)
	t.Setenv("AXILIO_TEST_MANPAGE", linkedRoff)

	root := Root()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"help", "--html"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("help --html wrote stderr: %s", stderr.String())
	}
	resolvedHTML, err := filepath.EvalSymlinks(stagedHTML)
	if err != nil {
		t.Fatal(err)
	}
	want := htmlManualFileURL(resolvedHTML) + "\n"
	if stdout.String() != want {
		t.Fatalf("help --html output = %q, want %q", stdout.String(), want)
	}
	if !strings.Contains(stdout.String(), "manual%20files") {
		t.Errorf("file URL does not escape spaces: %q", stdout.String())
	}
}

func TestHelpHTMLRejectsCommandName(t *testing.T) {
	root := Root()
	root.SetArgs([]string{"help", "phone", "--html"})
	err := root.ExecuteContext(context.Background())
	if err == nil {
		t.Fatal("help phone --html succeeded")
	}
	if got := exit.Classify(err); got != exit.Usage {
		t.Fatalf("help phone --html exit class = %d, want %d", got, exit.Usage)
	}
	if !strings.Contains(err.Error(), "cannot be combined") {
		t.Fatalf("help phone --html error = %q", err)
	}
}

// install.ps1 puts the HTML manual in a man directory next to axilio.exe, the
// first candidate for an executable outside a bin/share prefix lookup.
func TestHTMLManualCandidatesIncludeExecutableManDirectory(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "Programs", "axilio", "bin")
	candidates := appendExecutableManualCandidates(nil, filepath.Join(directory, "axilio.exe"))
	want := filepath.Join(directory, "man", htmlManualName)
	if len(candidates) == 0 || candidates[0] != want {
		t.Fatalf("candidates = %q, want %q first", candidates, want)
	}
}

func TestHTMLManualFileURLOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("drive-letter paths only parse on Windows")
	}
	got := htmlManualFileURL(`C:\Users\Ada Lovelace\AppData\Local\Programs\axilio\bin\man\axilio.1.html`)
	want := "file:///C:/Users/Ada%20Lovelace/AppData/Local/Programs/axilio/bin/man/axilio.1.html"
	if got != want {
		t.Fatalf("htmlManualFileURL = %q, want %q", got, want)
	}
}
