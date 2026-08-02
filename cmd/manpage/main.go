// Command manpage writes or verifies the checked-in axilio(1) page.
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
	generated, err := cmd.GenerateManpage(cmd.Root(), strings.TrimSpace(string(versionBytes)))
	if err != nil {
		fatalf("generate %s: %v", output, err)
	}

	if check {
		checkedIn, err := os.ReadFile(output)
		if err != nil {
			fatalf("read %s: %v; run `go generate ./...`", output, err)
		}
		if !bytes.Equal(generated, checkedIn) {
			fatalf("%s is stale; run `go generate ./...`", output)
		}
		return
	}

	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		fatalf("create manpage directory: %v", err)
	}
	if err := os.WriteFile(output, generated, 0o644); err != nil {
		fatalf("write %s: %v", output, err)
	}
}

func fatalf(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stderr, "manpage: "+format+"\n", args...)
	os.Exit(1)
}
