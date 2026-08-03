package cmd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateManpageHTMLDeterministicAndComplete(t *testing.T) {
	requireMandoc(t)
	version := readManpageTestVersion(t)
	roff, err := GenerateManpage(Root(), version)
	if err != nil {
		t.Fatal(err)
	}
	first, err := GenerateManpageHTML(roff)
	if err != nil {
		t.Fatal(err)
	}
	second, err := GenerateManpageHTML(roff)
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
		"Generated from man/axilio.1 for axilio " + version,
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
		`<pre class="command-synopsis"><code>axilio api-keys create &lt;name&gt;`,
		`<code class="language-console">user@host ~ % axilio doctor`,
		"axilio api-&lt;Tab&gt;",
		"background-color: #f5f5f5",
		"background-color: #f6f6f6",
		"border-left: 3px solid #b0b0b0",
		"pre.command-synopsis code { color: #181818; font-weight: normal; }",
		"border-left: 4px solid #008000",
		"white-space: pre",
		"overflow-x: auto",
		"code.language-console::first-line",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("generated HTML missing %q", want)
		}
	}
	normalizedPage := strings.Join(strings.Fields(page), " ")
	for _, want := range []string{
		"complete command names, subcommands, and flags",
		"resource IDs and names are not fetched dynamically",
	} {
		if !strings.Contains(normalizedPage, want) {
			t.Errorf("generated HTML missing prose %q", want)
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
	if got, want := strings.Count(page, `<pre class="command-synopsis"><code>`), len(publicCommands(root)); got != want {
		t.Errorf("styled command synopsis count = %d, want %d", got, want)
	}
	apiKeys := strings.Index(page, `<h3 id="COMMAND_api-keys">api-keys</h3>`)
	list := strings.Index(page, `<h4 id="COMMAND_api-keys-list">list</h4>`)
	create := strings.Index(page, `<h4 id="COMMAND_api-keys-create">create</h4>`)
	deleteCommand := strings.Index(page, `<h4 id="COMMAND_api-keys-delete">delete</h4>`)
	if apiKeys < 0 || list <= apiKeys || create <= list || deleteCommand <= create {
		t.Errorf("HTML command hierarchy is not api-keys -> list -> create -> delete")
	}
	for _, unwanted := range []string{
		"<script",
		`rel="stylesheet"`,
		"javascript:",
		"<session-id>",
		"__axilio_debug",
		"#compdef axilio",
	} {
		if strings.Contains(page, unwanted) {
			t.Errorf("generated HTML contains unsafe or non-self-contained value %q", unwanted)
		}
	}
}

func TestGeneratedManpageHTMLMatchesCheckedInPage(t *testing.T) {
	requireMandoc(t)
	roffPath := filepath.Join("..", "man", "axilio.1")
	roff, err := os.ReadFile(roffPath)
	if err != nil {
		t.Fatalf("read %s: %v; run `go generate ./...`", roffPath, err)
	}
	generated, err := GenerateManpageHTML(roff)
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
	for name, manpage := range map[string][]byte{
		"nil":            nil,
		"empty":          []byte("  \n"),
		"missing header": []byte(".SH NAME\naxilio\n"),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := GenerateManpageHTML(manpage); err == nil {
				t.Fatal("GenerateManpageHTML returned no error")
			}
		})
	}
}

func requireMandoc(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("mandoc"); err != nil {
		t.Skip("mandoc not installed")
	}
}
