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
		htmlOnly    bool
		versionFile string
	)
	flag.BoolVar(&check, "check", false, "verify the checked-in page without writing it")
	flag.BoolVar(&htmlOnly, "html-only", false, "convert an existing roff page to HTML")
	flag.StringVar(&versionFile, "version-file", "VERSION", "path to the source version file")
	flag.Parse()

	if flag.NArg() > 1 {
		fatalf("usage: go run ./cmd/manpage [--check] [--html-only] [--version-file VERSION] [man/axilio.1]")
	}
	output := "man/axilio.1"
	if flag.NArg() == 1 {
		output = flag.Arg(0)
	}
	if htmlOnly {
		generateHTML(output, check)
		return
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
	if check {
		checkGenerated(output, generated)
		return
	}

	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		fatalf("create manpage directory: %v", err)
	}
	writeGenerated(output, generated)
}

func generateHTML(manpage string, check bool) {
	roff, err := os.ReadFile(manpage)
	if err != nil {
		fatalf("read %s: %v", manpage, err)
	}
	generated, err := cmd.GenerateManpageHTML(roff)
	if err != nil {
		fatalf("generate %s.html: %v", manpage, err)
	}
	output := manpage + ".html"
	if check {
		checkGenerated(output, generated)
		return
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
