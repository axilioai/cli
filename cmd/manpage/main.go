// Command manpage writes or verifies the generated axilio(1) manual pages.
// Both formats render from the same command tree: the roff page is checked in,
// and the HTML page is a build artifact produced at package time.
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
		asHTML      bool
		versionFile string
	)
	flag.BoolVar(&check, "check", false, "verify the existing page without writing it")
	flag.BoolVar(&asHTML, "html", false, "render the browser manual instead of the roff page")
	flag.StringVar(&versionFile, "version-file", "VERSION", "path to the source version file")
	flag.Parse()

	if flag.NArg() > 1 {
		fatalf("usage: go run ./cmd/manpage [--check] [--html] [--version-file VERSION] [output]")
	}
	output := "man/axilio.1"
	if asHTML {
		output += ".html"
	}
	if flag.NArg() == 1 {
		output = flag.Arg(0)
	}

	versionBytes, err := os.ReadFile(versionFile)
	if err != nil {
		fatalf("read version file %s: %v", versionFile, err)
	}
	version := strings.TrimSpace(string(versionBytes))

	generate := cmd.GenerateManpage
	if asHTML {
		generate = cmd.GenerateManpageHTML
	}
	generated, err := generate(cmd.Root(), version)
	if err != nil {
		fatalf("generate %s: %v", output, err)
	}

	if check {
		checkGenerated(output, generated)
		return
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		fatalf("create manpage directory: %v", err)
	}
	writeGenerated(output, generated)
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
