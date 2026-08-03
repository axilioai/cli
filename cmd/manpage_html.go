package cmd

import (
	"bytes"
	"fmt"
	"html"
	"html/template"
	"os/exec"
	"regexp"
	"strings"
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

var (
	manpageTitlePattern    = regexp.MustCompile(`(?m)^\.TH "AXILIO" "1" "([^"]+)" "axilio ([^"]+)" "Axilio CLI Manual"$`)
	mandocHeadingPattern   = regexp.MustCompile(`(?s)<h([12]) class="(Sh|Ss)" id="([^"]+)"><a class="permalink" href="#[^"]+">(.*?)</a></h[12]>`)
	mandocTagPattern       = regexp.MustCompile(`<[^>]+>`)
	mandocPrePattern       = regexp.MustCompile(`(?s)<pre>(.*?)</pre>`)
	commandSynopsisPattern = regexp.MustCompile(`(?s)(<h[34] id="COMMAND_[^"]+">[^<]*</h[34]>\s*)<pre>(axilio .*?)</pre>`)
	terminalBlockPattern   = regexp.MustCompile(`(?s)<pre>(user@host .*?)</pre>`)
	trailingSpacePattern   = regexp.MustCompile(`(?m)[\t ]+$`)
)

// GenerateManpageHTML converts the exact roff page shipped in release
// artifacts into a self-contained browser manual. mandoc is the sole semantic
// renderer; this function only restores the browser hierarchy, navigation, and
// styling that surround its HTML fragment.
func GenerateManpageHTML(manpage []byte) ([]byte, error) {
	metadata := manpageTitlePattern.FindSubmatch(manpage)
	if metadata == nil {
		return nil, fmt.Errorf("generate manpage HTML: invalid or missing AXILIO .TH header")
	}

	mandoc := exec.Command("mandoc", "-Thtml", "-Ofragment")
	mandoc.Stdin = bytes.NewReader(manpage)
	var stderr bytes.Buffer
	mandoc.Stderr = &stderr
	fragment, err := mandoc.Output()
	if err != nil {
		return nil, fmt.Errorf("generate manpage HTML with mandoc: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	body, err := manpageHTMLBody(fragment)
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
		Version:    string(metadata[2]),
		SourceDate: string(metadata[1]),
		Sections:   manpageHTMLSections,
		// mandoc generated this fragment from the checked-in roff source.
		Body: template.HTML(body), //nolint:gosec
	})
	if err != nil {
		return nil, fmt.Errorf("generate manpage HTML: %w", err)
	}
	return output.Bytes(), nil
}

func manpageHTMLBody(fragment []byte) ([]byte, error) {
	const open = `<div class="manual-text">`
	start := bytes.Index(fragment, []byte(open))
	end := bytes.LastIndex(fragment, []byte(`</div>`))
	if start < 0 || end < start {
		return nil, fmt.Errorf("generate manpage HTML: mandoc output has no manual body")
	}
	page := string(fragment[start+len(open) : end])
	page = rewriteMandocHeadings(page)
	page = mandocPrePattern.ReplaceAllStringFunc(page, func(block string) string {
		return strings.ReplaceAll(block, "\n<br/>\n", "\n")
	})
	page = commandSynopsisPattern.ReplaceAllString(page, `$1<pre class="command-synopsis"><code>$2</code></pre>`)
	page = terminalBlockPattern.ReplaceAllString(page, `<pre class="terminal"><code class="language-console">$1</code></pre>`)
	page = strings.ReplaceAll(page, `<p class="Pp"></p>`, "")
	for _, url := range []string{
		"https://docs.axilio.ai",
		"https://github.com/axilioai/cli/issues",
		"https://axilio.ai",
	} {
		page = strings.ReplaceAll(page, url, `<a href="`+url+`">`+url+`</a>`)
	}
	page = trailingSpacePattern.ReplaceAllString(page, "")
	return []byte(strings.TrimSpace(page) + "\n"), nil
}

func rewriteMandocHeadings(page string) string {
	matches := mandocHeadingPattern.FindAllStringSubmatchIndex(page, -1)
	if len(matches) == 0 {
		return page
	}
	mainSections := make(map[string]bool, len(manpageHTMLSections))
	for _, section := range manpageHTMLSections {
		mainSections[section.ID] = true
	}

	var output strings.Builder
	last := 0
	inCommands := false
	commandFamily := ""
	for i, match := range matches {
		output.WriteString(page[last:match[0]])
		class := page[match[4]:match[5]]
		id := page[match[6]:match[7]]
		label := compactHTMLText(page[match[8]:match[9]])

		switch {
		case class == "Sh" && mainSections[id]:
			inCommands = id == "COMMANDS"
			commandFamily = ""
			fmt.Fprintf(&output, `<h2 id="%s">%s &nbsp; &nbsp; &nbsp; &nbsp; <a href="#top_of_page"><span class="top-link">top</span></a></h2>`, id, html.EscapeString(label))
		case class == "Sh" && inCommands:
			commandFamily = label
			fmt.Fprintf(&output, `<h3 id="COMMAND_%s">%s</h3>`, anchorPart(label), html.EscapeString(label))
		case class == "Ss" && inCommands && commandReferenceFollows(page, matches, i, commandFamily, label):
			fmt.Fprintf(&output, `<h4 id="COMMAND_%s-%s">%s</h4>`, anchorPart(commandFamily), anchorPart(label), html.EscapeString(label))
		case class == "Ss" && inCommands:
			fmt.Fprintf(&output, `<h5 id="%s">%s</h5>`, html.EscapeString(id), html.EscapeString(label))
		case class == "Ss":
			fmt.Fprintf(&output, `<h3 id="%s">%s</h3>`, html.EscapeString(id), html.EscapeString(label))
		default:
			output.WriteString(page[match[0]:match[1]])
		}
		last = match[1]
	}
	output.WriteString(page[last:])
	return output.String()
}

func commandReferenceFollows(page string, matches [][]int, index int, family, command string) bool {
	end := len(page)
	if index+1 < len(matches) {
		end = matches[index+1][0]
	}
	section := page[matches[index][1]:end]
	preStart := strings.Index(section, "<pre>")
	if preStart < 0 {
		return false
	}
	preStart += len("<pre>")
	preEnd := strings.Index(section[preStart:], "</pre>")
	if preEnd < 0 {
		return false
	}
	invocation := html.UnescapeString(section[preStart : preStart+preEnd])
	invocation = strings.TrimSpace(strings.SplitN(invocation, "\n", 2)[0])
	want := "axilio " + family + " " + command
	return invocation == want || strings.HasPrefix(invocation, want+" ")
}

func compactHTMLText(value string) string {
	value = mandocTagPattern.ReplaceAllString(value, "")
	return strings.Join(strings.Fields(html.UnescapeString(value)), " ")
}

func anchorPart(value string) string {
	return strings.ReplaceAll(strings.TrimSpace(value), " ", "-")
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
    main.manual-text section > p, main.manual-text section > ul, main.manual-text section > ol, main.manual-text section > dl, main.manual-text section > pre { margin-left: 64px; max-width: 686px; }
    main.manual-text p.Pp:empty { display: none; }
    pre.command-synopsis { box-sizing: border-box; margin-top: 0.45em; margin-bottom: 0.9em; padding: 8px 11px; border: 1px solid #dedede; border-left: 3px solid #b0b0b0; background-color: #f6f6f6; line-height: 1.35; white-space: pre; overflow-x: auto; overflow-wrap: normal; }
    pre.command-synopsis code { color: #181818; font-weight: normal; }
    code.language-console { display: block; box-sizing: border-box; padding: 12px 14px; border: 1px solid #d8d8d8; border-left: 4px solid #008000; background-color: #f5f5f5; color: #181818; line-height: 1.45; white-space: pre; overflow-x: auto; overflow-wrap: normal; box-shadow: inset 0 0 0 1px #fff; }
    code.language-console::first-line { color: #006000; font-weight: bold; }
    main.manual-text h3 + p, main.manual-text h3 + pre, main.manual-text h4 + p, main.manual-text h4 + pre, main.manual-text h5 + p, main.manual-text h5 + pre { margin-left: 64px; }
    main.manual-text dl dt { margin-top: 0.6em; }
    main.manual-text dl dd { margin-left: 32px; }
    .footer p { margin-top: 0.7em; margin-bottom: 0.7em; }
    @media (max-width: 760px) {
      td.training-cell { display: none; }
      main.manual-text section > p, main.manual-text section > ul, main.manual-text section > ol, main.manual-text section > dl, main.manual-text section > pre { margin-left: 24px; max-width: calc(100% - 32px); }
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
  <div class="footer"><p>Generated from man/axilio.1 for axilio {{ .Version }} · source date {{ .SourceDate }} · <a href="#top_of_page">top</a></p></div>
</body>
</html>
`))
