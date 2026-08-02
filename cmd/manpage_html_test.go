package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
		`<html lang="en-US">`,
		"<title>axilio(1) - CLI manual page</title>",
		`<div class="nav-bar">`,
		`<p class="section-dir">`,
		`<span class="headline">`,
		"<i>AXILIO</i>(1)",
		"Axilio CLI Manual",
		"axilio " + version + " command tree",
		"https://docs.axilio.ai",
		"https://github.com/axilioai/cli/issues",
		"&lt;session-id&gt;",
		"background-color: #fcfcfc",
		"color: #008000",
		"color: #A00000",
		"color: #502000",
		"color: #1030ff",
		"background-color: #ffe0e0",
		`<span class="top-link">top</span>`,
		`<code class="language-console">user@host ~ % axilio doctor`,
		"background-color: #f5f5f5",
		"border-left: 4px solid #008000",
		"white-space: pre",
		"overflow-x: auto",
		"code.language-console::first-line",
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
	root := Root()
	for _, command := range publicCommands(root) {
		anchor := commandHTMLAnchor(root, command)
		if !strings.Contains(page, `id="`+anchor+`"`) {
			t.Errorf("generated HTML missing public command anchor %q", anchor)
		}
	}
	apiKeys := strings.Index(page, `<h3 id="COMMAND_api-keys">api-keys</h3>`)
	create := strings.Index(page, `<h4 id="COMMAND_api-keys-create">create</h4>`)
	deleteCommand := strings.Index(page, `<h4 id="COMMAND_api-keys-delete">delete</h4>`)
	list := strings.Index(page, `<h4 id="COMMAND_api-keys-list">list</h4>`)
	if apiKeys < 0 || create <= apiKeys || deleteCommand <= create || list <= deleteCommand {
		t.Errorf("HTML command hierarchy is not api-keys -> create -> delete -> list")
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
