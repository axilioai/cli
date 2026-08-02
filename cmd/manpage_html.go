package cmd

import (
	"bytes"
	"fmt"
	"html/template"
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

// GenerateManpageHTML renders a self-contained browser manual from the same
// validated Markdown source as GenerateManpage. It contains no script, remote
// stylesheet, wall-clock value, or host-specific input.
func GenerateManpageHTML(root *cobra.Command, version string) ([]byte, error) {
	markdown, err := generateValidatedManpageMarkdown(root, version)
	if err != nil {
		return nil, err
	}

	// The first line is md2man's title block. The browser shell renders the
	// equivalent metadata itself, so exclude it from the semantic page body.
	if lineEnd := strings.IndexByte(markdown, '\n'); lineEnd >= 0 {
		markdown = markdown[lineEnd+1:]
	}
	renderer := blackfriday.NewHTMLRenderer(blackfriday.HTMLRendererParameters{
		HeadingLevelOffset: 1,
		Flags:              blackfriday.SkipHTML | blackfriday.Safelink | blackfriday.NoreferrerLinks | blackfriday.NoopenerLinks,
	})
	body := blackfriday.Run(
		[]byte(markdown),
		blackfriday.WithRenderer(renderer),
		blackfriday.WithExtensions(blackfriday.CommonExtensions|blackfriday.AutoHeadingIDs|blackfriday.HeadingIDs),
	)
	body = addHTMLTopLinks(body)

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
		// Blackfriday generated this fragment from repository-owned Markdown
		// with raw HTML explicitly disabled on the renderer.
		Body: template.HTML(body), //nolint:gosec
	})
	if err != nil {
		return nil, fmt.Errorf("generate manpage HTML: %w", err)
	}
	return output.Bytes(), nil
}

func addHTMLTopLinks(body []byte) []byte {
	page := string(body)
	for _, section := range manpageHTMLSections {
		heading := fmt.Sprintf(`<h2 id="%s">%s</h2>`, section.ID, section.Label)
		withTopLink := fmt.Sprintf(`<h2 id="%s">%s &nbsp; &nbsp; &nbsp; &nbsp; <a href="#top_of_page"><span class="top-link">top</span></a></h2>`, section.ID, section.Label)
		page = strings.Replace(page, heading, withTopLink, 1)
	}
	return []byte(page)
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
    code.language-console { display: block; box-sizing: border-box; padding: 12px 14px; border: 1px solid #d8d8d8; border-left: 4px solid #008000; background-color: #f5f5f5; color: #181818; line-height: 1.45; white-space: pre; overflow-x: auto; overflow-wrap: normal; box-shadow: inset 0 0 0 1px #fff; }
    code.language-console::first-line { color: #006000; font-weight: bold; }
    main.manual-text h3 + p, main.manual-text h3 + pre, main.manual-text h4 + p, main.manual-text h4 + pre { margin-left: 64px; }
    main.manual-text dl dt { margin-top: 0.6em; }
    main.manual-text dl dd { margin-left: 32px; }
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
  <div class="footer"><p>Generated from the axilio {{ .Version }} command tree · source date {{ .SourceDate }} · <a href="#top_of_page">top</a></p></div>
</body>
</html>
`))
