package cmd

import (
	"bytes"
	"fmt"
	"html"
	"html/template"
	"io"
	"strings"

	"github.com/russross/blackfriday/v2"
	"github.com/spf13/cobra"
)

type manpageHTMLSection struct {
	ID    string
	Label string
}

var manpageHTMLSections = []manpageHTMLSection{
	{ID: "NAME", Label: "NAME"},
	{ID: "SYNOPSIS", Label: "SYNOPSIS"},
	{ID: "DESCRIPTION", Label: "DESCRIPTION"},
	{ID: "COMMON_WORKFLOW", Label: "COMMON WORKFLOW"},
	{ID: "GLOBAL_OPTIONS", Label: "GLOBAL OPTIONS"},
	{ID: "COMMANDS", Label: "COMMANDS"},
	{ID: "ENVIRONMENT", Label: "ENVIRONMENT"},
	{ID: "EXIT_STATUS", Label: "EXIT STATUS"},
	{ID: "FILES", Label: "FILES"},
	{ID: "NOTES", Label: "NOTES"},
	{ID: "EXAMPLES", Label: "EXAMPLES"},
	{ID: "SEE_ALSO", Label: "SEE ALSO"},
}

// manpageHTMLLinkedURLs are linked where they appear in manual prose. The
// Autolink extension is deliberately off: API hosts appear throughout the text
// as values rather than destinations, and linking those would be noise.
var manpageHTMLLinkedURLs = []string{
	"https://docs.axilio.ai",
	"https://github.com/axilioai/cli/issues",
	"https://axilio.ai",
}

// manpageHTMLExtensions mirror the Markdown dialect the roff renderer parses,
// so both formats read the source the same way. NoIntraEmphasis matters most:
// without it, identifiers like AXILIO_API_KEY would turn their underscores
// into emphasis. Autolink is deliberately absent -- see
// manpageHTMLLinkedURLs.
const manpageHTMLExtensions = blackfriday.NoIntraEmphasis |
	blackfriday.FencedCode |
	blackfriday.SpaceHeadings |
	blackfriday.DefinitionLists |
	blackfriday.HeadingIDs

// GenerateManpageHTML renders the browser manual from the same Markdown that
// produces man/axilio.1. Both formats therefore derive from one source, and
// neither has to parse the other's output.
func GenerateManpageHTML(root *cobra.Command, version string) ([]byte, error) {
	markdown, err := generateValidatedManpageMarkdown(root, version)
	if err != nil {
		return nil, err
	}

	var output bytes.Buffer
	err = manpageHTMLTemplate.Execute(&output, struct {
		Version    string
		SourceDate string
		Sections   []manpageHTMLSection
		Body       template.HTML
	}{
		Version:    strings.TrimSpace(version),
		SourceDate: manpageSourceDate,
		Sections:   manpageHTMLSections,
		// Rendered from the generator's own Markdown, not from user input.
		Body: template.HTML(manpageHTMLBody(markdown)), //nolint:gosec
	})
	if err != nil {
		return nil, fmt.Errorf("generate manpage HTML: %w", err)
	}
	return output.Bytes(), nil
}

func manpageHTMLBody(markdown string) string {
	// The pandoc-style title line feeds roff's .TH; the page template owns the
	// same metadata for HTML.
	if _, rest, found := strings.Cut(markdown, "\n"); found && strings.HasPrefix(markdown, "% ") {
		markdown = rest
	}

	rendered := blackfriday.Run(
		[]byte(markdown),
		blackfriday.WithRenderer(&manpageHTMLRenderer{
			HTMLRenderer: blackfriday.NewHTMLRenderer(blackfriday.HTMLRendererParameters{}),
		}),
		blackfriday.WithExtensions(manpageHTMLExtensions),
	)

	body := string(rendered)
	for _, url := range manpageHTMLLinkedURLs {
		body = strings.ReplaceAll(body, url, `<a href="`+url+`">`+url+`</a>`)
	}
	return strings.TrimSpace(body) + "\n"
}

// manpageHTMLRenderer adapts Blackfriday's HTML output to the manual page's
// document outline. Headings and literal blocks carry structure the generator
// already knows, so they are emitted directly; everything else uses the
// stock renderer.
type manpageHTMLRenderer struct {
	*blackfriday.HTMLRenderer
}

func (r *manpageHTMLRenderer) RenderNode(w io.Writer, node *blackfriday.Node, entering bool) blackfriday.WalkStatus {
	switch node.Type {
	case blackfriday.Heading:
		level := manpageHTMLHeadingLevel(node.Level, node.HeadingID)
		if entering {
			fmt.Fprintf(w, "\n<h%d id=%q>", level, node.HeadingID)
			return blackfriday.GoToNext
		}
		// Only the manual's top-level sections are in the page navigation, so
		// only they get a link back to it.
		if node.Level == 1 {
			io.WriteString(w, ` &nbsp; &nbsp; &nbsp; &nbsp; `+
				`<a href="#top_of_page"><span class="top-link">top</span></a>`)
		}
		fmt.Fprintf(w, "</h%d>\n", level)
		return blackfriday.GoToNext

	case blackfriday.CodeBlock:
		literal := html.EscapeString(strings.TrimRight(string(node.Literal), "\n"))
		switch string(node.CodeBlockData.Info) {
		case "synopsis":
			fmt.Fprintf(w, "<pre class=\"command-synopsis\"><code>%s</code></pre>\n", literal)
		case "console":
			fmt.Fprintf(w, "<pre class=\"terminal\"><code class=\"language-console\">%s</code></pre>\n", literal)
		default:
			fmt.Fprintf(w, "<pre>%s</pre>\n", literal)
		}
		return blackfriday.GoToNext
	}

	return r.HTMLRenderer.RenderNode(w, node, entering)
}

// manpageHTMLHeadingLevel maps a Markdown heading onto the page outline. The
// Markdown levels are chosen for roff, where md2man renders levels 1 and 2 as
// .SH and 3 and 4 as .SS; the browser page instead nests by meaning, so a
// command family sits one level under COMMANDS and its subcommands one below
// that. Only command headings carry depth; every other subsection belongs
// directly to the section above it.
func manpageHTMLHeadingLevel(markdownLevel int, id string) int {
	if strings.HasPrefix(id, "COMMAND_") {
		return markdownLevel + 1
	}
	if markdownLevel == 1 {
		return 2
	}
	return 3
}

var manpageHTMLTemplate = template.Must(template.New("axilio-manpage").Parse(`<!doctype html>
<html lang="en-US">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta name="generator" content="axilio manpage generator">
  <title>axilio(1) - CLI manual page</title>
  <style>
    html, body { background-color: #fcfcfc; font-family: sans-serif; font-size: 100%; color: #181818; }
    body { margin: 0; background-color: #fff; }
    h1, h2, h3, h4, h5, h6 { font-family: helvetica, sans-serif; font-weight: normal; margin-left: 8px; margin-right: 8px; color: #008000; margin-top: 25px; }
    h2 { color: #A00000; padding-top: 15px; font-size: 100%; font-weight: bold; }
    h3 { color: #600000; font-size: 100%; padding-top: 10px; padding-left: 20px; font-style: italic; }
    h4 { color: #502000; font-size: 100%; padding-top: 8px; padding-left: 40px; font-weight: bold; }
    h5 { color: #502000; font-size: 95%; padding-top: 6px; padding-left: 56px; font-style: italic; }
    p { margin-left: 8px; margin-right: 8px; margin-bottom: 0.5em; max-width: 750px; }
    table { max-width: 750px; }
    hr { max-width: 750px; margin: 8px; }
    pre { margin-left: 8px; font-family: monospace, courier; white-space: pre-wrap; overflow-wrap: anywhere; }
    li { max-width: 710px; margin-left: 8px; margin-right: 8px; }
    a { color: #1030ff; text-decoration: none; }
    a:visited { color: #4080dd; }
    a:hover, a:focus, a:active { color: red; background-color: #ffe0e0; text-decoration: underline; }
    strong, b { font-weight: bold; color: #502000; }
    em, i { color: #006000; }
    code { font-family: monospace, courier; }
    /* Inline code carries the same weight and color as other emphasized
       terms; code inside a literal block keeps that block's styling. */
    :not(pre) > code { font-weight: bold; color: #502000; }
    div.nav-bar, div.footer { padding: 3px 8px; background-color: #e8e8e8; }
    table.nav-table { width: 100%; max-width: none; border-spacing: 0; border-width: 0; border-collapse: collapse; padding: 0; }
    td.nav-cell, td.training-cell { padding: 0; border-width: 0; }
    td.training-cell { text-align: right; }
    p.nav-text, p.training-text { margin: 0; font-size: 15px; }
    p.training-text { font-weight: bold; }
    a.training-link:hover, a.training-link:visited, a.training-link:link, a.training-link:active { color: #008000; }
    a.training-link:hover, a.training-link:active { text-decoration: underline; background-color: #ffd0d0; }
    hr.nav-end { height: 0; margin-top: 0; color: #0000ff; border-color: #fff; width: 100%; max-width: none; }
    table.sec-table { width: 100%; border: 1px; }
    p.section-dir { margin-top: 6px; margin-bottom: 6px; padding: 5px; border-width: 1px; }
    p.section-dir a { white-space: nowrap; }
    span.headline, span.footline { font-weight: bold; }
    span.top-link { font-size: 70%; }
    main.manual-text { font-family: monospace, courier; }
    main.manual-text > p, main.manual-text > ul, main.manual-text > ol, main.manual-text > dl, main.manual-text > pre { margin-left: 64px; max-width: 686px; }
    main.manual-text dl dt { margin-top: 0.6em; }
    main.manual-text dl dd { margin-left: 32px; }
    /* Blank lines between list items make Markdown lists loose; keep them as
       tightly spaced as the surrounding reference material. */
    main.manual-text li > p { margin: 0; max-width: none; }
    pre.command-synopsis { box-sizing: border-box; margin-top: 0.45em; margin-bottom: 0.9em; padding: 8px 11px; border: 1px solid #dedede; border-left: 3px solid #b0b0b0; background-color: #f6f6f6; line-height: 1.35; white-space: pre; overflow-x: auto; overflow-wrap: normal; }
    pre.command-synopsis code { color: #181818; font-weight: normal; }
    code.language-console { display: block; box-sizing: border-box; padding: 12px 14px; border: 1px solid #d8d8d8; border-left: 4px solid #008000; background-color: #f5f5f5; color: #181818; line-height: 1.45; white-space: pre; overflow-x: auto; overflow-wrap: normal; box-shadow: inset 0 0 0 1px #fff; }
    code.language-console::first-line { color: #006000; font-weight: bold; }
    main.manual-text h3 + p, main.manual-text h3 + pre, main.manual-text h4 + p, main.manual-text h4 + pre, main.manual-text h5 + p, main.manual-text h5 + pre { margin-left: 64px; }
    .footer p { margin-top: 0.7em; margin-bottom: 0.7em; }
    @media (max-width: 760px) {
      td.training-cell { display: none; }
      main.manual-text > p, main.manual-text > ul, main.manual-text > ol, main.manual-text > dl, main.manual-text > pre { margin-left: 24px; max-width: calc(100% - 32px); }
      h3 { padding-left: 8px; }
      h4 { padding-left: 16px; }
    }
    @media print { .nav-bar, .sec-table, .footer, .top-link { display: none; } a { color: inherit; } }
  </style>
</head>
<body>
  <div class="page-top"><a id="top_of_page"></a></div>
  <div class="nav-bar">
    <table class="nav-table">
      <tr>
        <td class="nav-cell"><p class="nav-text"><a href="https://axilio.ai">axilio.ai</a> &gt; CLI &gt; Manual</p></td>
        <td class="training-cell"><p class="training-text"><a class="training-link" href="https://docs.axilio.ai">Axilio documentation</a></p></td>
      </tr>
    </table>
  </div>
  <hr class="nav-end">
  <h1>axilio(1) &mdash; CLI manual page</h1>
  <table class="sec-table">
    <tr>
      <td><p class="section-dir">{{ range $index, $section := .Sections }}{{ if $index }} | {{ end }}<a href="#{{ $section.ID }}">{{ $section.Label }}</a>{{ end }}</p></td>
    </tr>
  </table>
  <pre><span class="headline"><i>AXILIO</i>(1)                     Axilio CLI Manual                     <i>AXILIO</i>(1)</span></pre>
  <main id="manual" class="manual-text">
{{ .Body }}  </main>
  <hr class="nav-end">
  <div class="footer"><p>Generated from the axilio command tree for axilio {{ .Version }} &middot; source date {{ .SourceDate }} &middot; <a href="#top_of_page">top</a></p></div>
</body>
</html>
`))
