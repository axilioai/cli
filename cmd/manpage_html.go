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
	{ID: "name", Label: "NAME"},
	{ID: "synopsis", Label: "SYNOPSIS"},
	{ID: "description", Label: "DESCRIPTION"},
	{ID: "common-workflow", Label: "COMMON WORKFLOW"},
	{ID: "global-options", Label: "GLOBAL OPTIONS"},
	{ID: "commands", Label: "COMMANDS"},
	{ID: "environment", Label: "ENVIRONMENT"},
	{ID: "exit-status", Label: "EXIT STATUS"},
	{ID: "files", Label: "FILES"},
	{ID: "examples", Label: "EXAMPLES"},
	{ID: "see-also", Label: "SEE ALSO"},
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
		Flags: blackfriday.SkipHTML | blackfriday.Safelink | blackfriday.NoreferrerLinks | blackfriday.NoopenerLinks,
	})
	body := blackfriday.Run(
		[]byte(markdown),
		blackfriday.WithRenderer(renderer),
		blackfriday.WithExtensions(blackfriday.CommonExtensions|blackfriday.AutoHeadingIDs),
	)

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

var manpageHTMLTemplate = template.Must(template.New("axilio-manpage").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta name="generator" content="axilio manpage generator">
  <title>axilio(1) — CLI manual</title>
  <style>
    :root { color-scheme: light; --ink: #111; --muted: #555; --rule: #b8b8b8; --link: #0645ad; --code: #f5f5f5; }
    * { box-sizing: border-box; }
    html { background: #fff; color: var(--ink); }
    body { max-width: 112ch; margin: 0 auto; padding: 1.5rem 2rem 4rem; font: 15px/1.45 ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace; }
    a { color: var(--link); text-decoration-thickness: 1px; text-underline-offset: 0.15em; }
    a:hover { text-decoration-thickness: 2px; }
    .breadcrumb, .page-title, .section-nav, footer { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; }
    .breadcrumb { margin: 0 0 0.8rem; color: var(--muted); font-size: 0.9rem; }
    .page-title { margin: 1.2rem 0 0.8rem; font-size: 1.65rem; line-height: 1.2; }
    .section-nav { display: flex; flex-wrap: wrap; gap: 0.25rem 0.45rem; margin: 0 0 1.5rem; font-size: 0.82rem; }
    .section-nav a { white-space: nowrap; }
    .manual-head { display: grid; grid-template-columns: 1fr auto 1fr; gap: 1rem; padding: 0.55rem 0; border-block: 1px solid var(--rule); font-weight: 700; }
    .manual-head span:nth-child(2) { text-align: center; }
    .manual-head span:last-child { text-align: right; }
    main { padding-top: 0.5rem; }
    main h1, main h2, main h3 { font: inherit; font-weight: 700; scroll-margin-top: 1rem; }
    main h1 { margin: 2.1rem 0 0.8rem; font-size: 1rem; text-transform: uppercase; }
    main h2 { margin: 2rem 0 0.8rem; font-size: 1rem; }
    main h3 { margin: 1.4rem 0 0.6rem 4ch; font-size: 0.95rem; }
    main p, main ul, main ol, main dl, main pre { margin-left: 7ch; }
    main p { margin-top: 0.45rem; margin-bottom: 0.75rem; }
    main ul, main ol { padding-left: 3ch; }
    main li { margin: 0.25rem 0; }
    main pre { overflow-x: auto; padding: 0.7rem 1ch; background: var(--code); white-space: pre-wrap; overflow-wrap: anywhere; }
    main code { font: inherit; }
    main :not(pre) > code { padding: 0.05rem 0.3ch; background: var(--code); }
    main dl dt { margin-top: 0.6rem; font-weight: 700; }
    main dl dd { margin-left: 4ch; }
    footer { margin-top: 3rem; padding-top: 1rem; border-top: 1px solid var(--rule); color: var(--muted); font-size: 0.8rem; }
    @media (max-width: 720px) {
      body { padding: 1rem; font-size: 13px; }
      .manual-head { grid-template-columns: 1fr auto; }
      .manual-head span:nth-child(2) { display: none; }
      main p, main ul, main ol, main dl, main pre { margin-left: 2ch; }
      main h3 { margin-left: 0; }
    }
    @media print {
      body { max-width: none; padding: 0; }
      .breadcrumb, .section-nav, footer { display: none; }
      a { color: inherit; text-decoration: none; }
    }
  </style>
</head>
<body id="top">
  <header>
    <p class="breadcrumb"><a href="https://axilio.ai">Axilio</a> &gt; CLI manual</p>
    <hr>
    <h1 class="page-title">axilio(1) — CLI manual</h1>
    <nav class="section-nav" aria-label="Manual sections">
      {{- range $index, $section := .Sections }}{{ if $index }}<span aria-hidden="true">|</span>{{ end }}<a href="#{{ $section.ID }}">{{ $section.Label }}</a>{{ end }}
    </nav>
    <div class="manual-head" aria-label="Manual title">
      <span>AXILIO(1)</span>
      <span>Axilio CLI Manual</span>
      <span>AXILIO(1)</span>
    </div>
  </header>
  <main id="manual">
{{ .Body }}  </main>
  <footer>
    <p>Generated from the axilio {{ .Version }} command tree · source date {{ .SourceDate }} · <a href="#top">top</a></p>
  </footer>
</body>
</html>
`))
