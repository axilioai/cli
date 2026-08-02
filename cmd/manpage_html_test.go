package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/russross/blackfriday/v2"
	"github.com/spf13/cobra"
)

func TestGenerateManpageHTMLDeterministicAndComplete(t *testing.T) {
	version := readManpageTestVersion(t)
	first, err := GenerateManpageHTML(Root(), version)
	if err != nil {
		t.Fatal(err)
	}
	second, err := GenerateManpageHTML(Root(), version)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("HTML generation is not deterministic across fresh Root calls")
	}

	page := string(first)
	for _, want := range []string{
		"<!doctype html>",
		`<html lang="en">`,
		"<title>axilio(1) — CLI manual</title>",
		`<nav class="section-nav" aria-label="Manual sections">`,
		`<div class="manual-head" aria-label="Manual title">`,
		"AXILIO(1)",
		"Axilio CLI Manual",
		"axilio " + version + " command tree",
		"https://docs.axilio.ai",
		"https://github.com/axilioai/cli/issues",
		"&lt;session-id&gt;",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("generated HTML missing %q", want)
		}
	}
	for _, section := range manpageHTMLSections {
		if !strings.Contains(page, `href="#`+section.ID+`"`) {
			t.Errorf("generated HTML navigation missing %q", section.Label)
		}
		if !strings.Contains(page, `id="`+section.ID+`"`) {
			t.Errorf("generated HTML body missing anchor %q", section.ID)
		}
	}
	for _, command := range publicCommands(Root()) {
		anchor := blackfriday.SanitizedAnchorName(command.CommandPath())
		if !strings.Contains(page, `id="`+anchor+`"`) {
			t.Errorf("generated HTML missing public command anchor %q", anchor)
		}
	}
	for _, unwanted := range []string{
		"<script",
		`rel="stylesheet"`,
		"javascript:",
		"<session-id>",
	} {
		if strings.Contains(page, unwanted) {
			t.Errorf("generated HTML contains unsafe or non-self-contained value %q", unwanted)
		}
	}
}

func TestGeneratedManpageHTMLMatchesCheckedInPage(t *testing.T) {
	generated, err := GenerateManpageHTML(Root(), readManpageTestVersion(t))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("..", "man", "axilio.1.html")
	checkedIn, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v; run `go generate ./...`", path, err)
	}
	if !bytes.Equal(generated, checkedIn) {
		t.Fatalf("%s is stale; run `go generate ./...`", path)
	}
}

func TestGenerateManpageHTMLRejectsInvalidInputs(t *testing.T) {
	for name, test := range map[string]struct {
		root    *cobra.Command
		version string
	}{
		"nil root":      {root: nil, version: "0.6.1"},
		"empty version": {root: Root(), version: "  "},
		"invalid version": {
			root:    Root(),
			version: "0.6.1\nnext",
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := GenerateManpageHTML(test.root, test.version); err == nil {
				t.Fatal("GenerateManpageHTML returned no error")
			}
		})
	}
}
