// Command manpage writes or verifies the checked-in axilio(1) roff and HTML pages.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/axilioai/cli/cmd"
)

func main() {
	var (
		check       bool
		versionFile string
	)
	flag.BoolVar(&check, "check", false, "verify the checked-in page without writing it")
	flag.StringVar(&versionFile, "version-file", "VERSION", "path to the source version file")
	flag.Parse()

	if flag.NArg() > 1 {
		fatalf("usage: go run ./cmd/manpage [--check] [--version-file VERSION] [man/axilio.1]")
	}
	output := "man/axilio.1"
	if flag.NArg() == 1 {
		output = flag.Arg(0)
	}

	versionBytes, err := os.ReadFile(versionFile)
	if err != nil {
		fatalf("read version file %s: %v", versionFile, err)
	}
	version := strings.TrimSpace(string(versionBytes))
	generated, err := cmd.GenerateManpage(cmd.Root(), version)
	if err != nil {
		fatalf("generate %s: %v", output, err)
	}
	htmlOutput := output + ".html"
	generatedHTML, err := cmd.GenerateManpageHTML(cmd.Root(), version)
	if err != nil {
		fatalf("generate %s: %v", htmlOutput, err)
	}

	if check {
		checkGenerated(output, generated)
		checkGenerated(htmlOutput, generatedHTML)
		return
	}

	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		fatalf("create manpage directory: %v", err)
	}
	writeGenerated(output, generated)
	writeGenerated(htmlOutput, generatedHTML)
}

func checkGenerated(path string, generated []byte) {
	checkedIn, err := os.ReadFile(path)
	if err != nil {
		fatalf("read %s: %v; run `go generate ./...`", path, err)
	}
	if !bytes.Equal(generated, checkedIn) {
		fatalf("%s is stale; run `go generate ./...`", path)
	}
}

func writeGenerated(path string, generated []byte) {
	if err := os.WriteFile(path, generated, 0o644); err != nil {
		fatalf("write %s: %v", path, err)
	}
}

func fatalf(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stderr, "manpage: "+format+"\n", args...)
	os.Exit(1)
}
