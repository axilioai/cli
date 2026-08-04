package cmd

import (
	"bytes"
	"os/exec"
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
		`<span class="top-link">top</span>`,
		`<pre class="command-synopsis"><code>axilio api-keys create &lt;name&gt;`,
		`<code class="language-console">user@host ~ % axilio doctor`,
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
