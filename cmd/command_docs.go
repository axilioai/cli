package cmd

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"
)

// commandDocumentationAnnotation is deliberately namespaced because Cobra
// annotations are shared with flag completion and other integrations.
const commandDocumentationAnnotation = "ai.axilio.cli/command-documentation.v1"

// CommandDocumentation is the shared source for interactive examples and the
// generated manual. It is serialized onto each Cobra command so every Root()
// call owns a complete, independent documentation tree.
type CommandDocumentation struct {
	Samples     []CommandSample `json:"samples"`
	Walkthrough string          `json:"walkthrough,omitempty"`
}

// CommandSample documents one representative invocation and its observable
// process contract. Output uses placeholders for values that vary by account,
// phone, run, release, or filesystem. For command failures, Stderr records the
// underlying error message; failedSample notes Fang's presentation layer.
type CommandSample struct {
	Invocation       string `json:"invocation"`
	Stdout           string `json:"stdout,omitempty"`
	Stderr           string `json:"stderr,omitempty"`
	ExitStatus       int    `json:"exit_status"`
	Notes            string `json:"notes,omitempty"`
	ExternalBehavior string `json:"external_behavior,omitempty"`
}

// AttachCommandDocumentation stores docs on cmd and derives Cobra's Example
// field. Fang therefore renders the same invocation and process contract as
// the manual generator without either renderer owning a second catalog.
func AttachCommandDocumentation(cmd *cobra.Command, docs CommandDocumentation) *cobra.Command {
	if cmd == nil {
		panic("attach command documentation to nil command")
	}
	encoded, err := json.Marshal(docs)
	if err != nil {
		panic(fmt.Sprintf("encode command documentation for %q: %v", cmd.Name(), err))
	}
	if cmd.Annotations == nil {
		cmd.Annotations = make(map[string]string)
	}
	cmd.Annotations[commandDocumentationAnnotation] = string(encoded)
	cmd.Example = renderCommandExamples(cmd, docs)

	if walkthrough := commandWalkthroughDescription(docs); walkthrough != "" &&
		!strings.Contains(cmd.Long, walkthrough) {
		cmd.Long = strings.TrimSpace(cmd.Long) +
			"\n\n" + walkthrough
	}
	return cmd
}

// commandLongWithoutWalkthrough returns the prose portion of Long. The manual
// renders Walkthrough as its own literal block, while Fang reads the combined
// Cobra Long field.
func commandLongWithoutWalkthrough(cmd *cobra.Command, docs CommandDocumentation) string {
	long := strings.TrimSpace(cmd.Long)
	walkthrough := commandWalkthroughDescription(docs)
	if walkthrough != "" && strings.HasSuffix(long, walkthrough) {
		return strings.TrimSpace(strings.TrimSuffix(long, walkthrough))
	}
	return long
}

func commandWalkthroughDescription(docs CommandDocumentation) string {
	walkthrough := strings.TrimSpace(docs.Walkthrough)
	if walkthrough == "" {
		return ""
	}
	return "Representative screen walkthrough (text, center, confidence):\n\n" +
		indentLiteral(walkthrough, "    ")
}

// CommandDocs reads the typed documentation attached to cmd. False means the
// annotation is absent or invalid; callers can treat either case as a coverage
// failure rather than silently rendering incomplete documentation.
func CommandDocs(cmd *cobra.Command) (CommandDocumentation, bool) {
	if cmd == nil || cmd.Annotations == nil {
		return CommandDocumentation{}, false
	}
	encoded, ok := cmd.Annotations[commandDocumentationAnnotation]
	if !ok {
		return CommandDocumentation{}, false
	}
	var docs CommandDocumentation
	if err := json.Unmarshal([]byte(encoded), &docs); err != nil {
		return CommandDocumentation{}, false
	}
	return docs, true
}

func renderCommandExamples(cmd *cobra.Command, docs CommandDocumentation) string {
	var rendered strings.Builder
	for i, sample := range docs.Samples {
		if i > 0 {
			rendered.WriteByte('\n')
		}
		rendered.WriteString(indentLiteral(strings.TrimSpace(sample.Invocation), "  "))
		if !cmd.Runnable() {
			continue
		}
		rendered.WriteByte('\n')
		if strings.TrimSpace(sample.ExternalBehavior) != "" {
			writeExampleComment(&rendered, "external behavior", sample.ExternalBehavior)
			continue
		}
		writeExampleComment(&rendered, "stdout", streamValue(sample.Stdout))
		writeExampleComment(&rendered, "stderr", streamValue(sample.Stderr))
		writeExampleComment(&rendered, "exit status", fmt.Sprintf("%d", sample.ExitStatus))
		if strings.TrimSpace(sample.Notes) != "" {
			writeExampleComment(&rendered, "note", sample.Notes)
		}
	}
	return strings.TrimRight(rendered.String(), "\n")
}

func streamValue(value string) string {
	if strings.TrimSpace(value) == "" {
		return "none"
	}
	return value
}

func writeExampleComment(out *strings.Builder, label, value string) {
	lines := strings.Split(strings.TrimSpace(value), "\n")
	if label == "note" {
		lines = wrapExampleComment(lines, 74)
	}
	first := true
	for _, line := range lines {
		prefix := "  # "
		if first {
			prefix += label + ": "
		}
		wrapped := []string{line}
		if utf8.RuneCountInString(prefix)+utf8.RuneCountInString(line) > 88 {
			wrapped = wrapExampleComment([]string{line}, 88-utf8.RuneCountInString(prefix))
		}
		for _, rendered := range wrapped {
			out.WriteString(prefix)
			out.WriteString(rendered)
			out.WriteByte('\n')
			first = false
			prefix = "  # "
		}
	}
}

func wrapExampleComment(lines []string, width int) []string {
	var wrapped []string
	for _, line := range lines {
		words := strings.Fields(line)
		if len(words) == 0 {
			wrapped = append(wrapped, "")
			continue
		}
		current := words[0]
		for _, word := range words[1:] {
			if utf8.RuneCountInString(current)+1+utf8.RuneCountInString(word) > width {
				wrapped = append(wrapped, current)
				current = word
				continue
			}
			current += " " + word
		}
		wrapped = append(wrapped, current)
	}
	return wrapped
}

func indentLiteral(value, indent string) string {
	lines := strings.Split(value, "\n")
	for i := range lines {
		lines[i] = indent + lines[i]
	}
	return strings.Join(lines, "\n")
}

func documentCommand(key string, cmd *cobra.Command) *cobra.Command {
	docs, ok := applicationCommandDocumentation[key]
	if !ok {
		panic("missing command documentation for " + key)
	}
	return AttachCommandDocumentation(cmd, docs)
}

func attachApplicationCommandDocumentation(root *cobra.Command) {
	var walk func(*cobra.Command)
	walk = func(command *cobra.Command) {
		key := commandHelpKey(command)
		if command == root {
			key = root.Name()
		}
		documentCommand(key, command)
		for _, child := range command.Commands() {
			if child.Name() == "help" || child.Name() == "completion" {
				continue
			}
			walk(child)
		}
	}
	walk(root)
}

func workflow(invocations ...string) CommandDocumentation {
	docs := CommandDocumentation{Samples: make([]CommandSample, 0, len(invocations))}
	for _, invocation := range invocations {
		docs.Samples = append(docs.Samples, CommandSample{Invocation: invocation})
	}
	return docs
}

func sample(invocation, stdout, stderr string) CommandSample {
	if stdout == "none" {
		stdout = ""
	}
	if stderr == "none" {
		stderr = ""
	}
	return CommandSample{Invocation: invocation, Stdout: stdout, Stderr: stderr}
}

func sampleWithNote(invocation, stdout, stderr, notes string) CommandSample {
	s := sample(invocation, stdout, stderr)
	s.Notes = notes
	return s
}

func failedSample(invocation, stdout, stderr string, status int, notes string) CommandSample {
	s := sampleWithNote(invocation, stdout, stderr, notes)
	s.ExitStatus = status
	return s
}

func externalSample(invocation, behavior string) CommandSample {
	return CommandSample{Invocation: invocation, ExternalBehavior: behavior}
}

const phoneObserveWalkthrough = `+----------------------------+
| Search                     |
| [ coffee shops           ] |  text "coffee shops"  540,620  0.99
|                            |
| [        Continue        ] |  text "Continue"     540,1120  0.98
+----------------------------+
right-side fields: text, center (x,y), confidence
frame 1080x2400; frame-space pixels; (0,0) is top-left

reuse the observed Continue center:
axilio phone tap 540 1120`

var applicationCommandDocumentation = map[string]CommandDocumentation{
	"axilio": workflow(
		"axilio login",
		`eval "$(axilio sessions start --export)"`,
		"axilio phone observe",
		`axilio phone tap --query "the search box"`,
		"axilio phone observe",
		"axilio sessions stop <session-id>",
	),
	"login": {Samples: []CommandSample{
		sampleWithNote("axilio login", "none", "→ Opening your browser to authorize the CLI…\n✓ Signed in to https://api.axilio.ai", "Representative stderr excerpt; the browser URL, optional organization line, and balance result vary by login."),
		sampleWithNote("axilio login --api-key axl_xxx", "none", "✓ Signed in to https://api.axilio.ai\n  Balance  $5.00\n  Saved to <config-path>", "axl_xxx is a non-secret format placeholder; replace it with a valid key before running the command."),
		sample(`printf '%s\n' "$AXILIO_API_KEY" | axilio login`, "none", "✓ Signed in to https://api.axilio.ai\n  Balance  $5.00\n  Saved to <config-path>"),
	}},
	"logout": {Samples: []CommandSample{
		sampleWithNote("axilio logout", "none", "✓ Signed out.", "Already-signed-out is also exit 0 and reports Already signed out."),
	}},
	"status": {Samples: []CommandSample{
		sample("axilio status", "Status      ok\nAPI host    https://api.axilio.ai/api/v1\nActive org  (session default)\nBalance     $5.00", "none"),
		sampleWithNote("axilio status -o json", "{\n  \"active_org\": \"\",\n  \"api_host\": \"https://api.axilio.ai/api/v1\",\n  \"balance\": \"$5.00\",\n  \"status\": \"ok\"\n}", "none", "Balance and active organization vary by account."),
	}},
	"doctor": {Samples: []CommandSample{
		sampleWithNote("axilio doctor", "CHECK           | STATUS | DETAIL\nAuthentication  | ok     | method: oauth (browser session)\nCredentials     | ok     | oauth session present\nConnectivity    | ok     | https://api.axilio.ai/api/v1 reachable\nAccount         | ok     | balance: $5.00\nPlan            | ok     | Hobby (active, up to 1 concurrent)\nCLI version     | ok     | 0.6.1 (<commit>) <go-version> <build-date>\nConfig file     | ok     | <config-path>\nSessions dir    | ok     | <sessions-directory>\nCurrent session | ok     | <session-id> (<phone-id>)", "none", "Representative authenticated report; balance, plan, build metadata, paths, and current session vary."),
		sampleWithNote("axilio doctor -o json", "{\n  \"checks\": [\n    {\n      \"name\": \"Authentication\",\n      \"status\": \"ok\",\n      \"detail\": \"method: oauth (browser session)\"\n    },\n    {\n      \"name\": \"Credentials\",\n      \"status\": \"ok\",\n      \"detail\": \"oauth session present\"\n    },\n    {\n      \"name\": \"Connectivity\",\n      \"status\": \"ok\",\n      \"detail\": \"https://api.axilio.ai/api/v1 reachable\"\n    },\n    {\n      \"name\": \"Account\",\n      \"status\": \"ok\",\n      \"detail\": \"balance: $5.00\"\n    },\n    {\n      \"name\": \"Plan\",\n      \"status\": \"ok\",\n      \"detail\": \"Hobby (active, up to 1 concurrent)\"\n    },\n    {\n      \"name\": \"CLI version\",\n      \"status\": \"ok\",\n      \"detail\": \"0.6.1 (<commit>) <go-version> <build-date>\"\n    },\n    {\n      \"name\": \"Config file\",\n      \"status\": \"ok\",\n      \"detail\": \"<config-path>\"\n    },\n    {\n      \"name\": \"Sessions dir\",\n      \"status\": \"ok\",\n      \"detail\": \"<sessions-directory>\"\n    },\n    {\n      \"name\": \"Current session\",\n      \"status\": \"ok\",\n      \"detail\": \"<session-id> (<phone-id>)\"\n    }\n  ],\n  \"ok\": true\n}", "none", "The JSON contains the same checks and representative values as the table; account and local values vary."),
		failedSample("AXILIO_API_KEY=axl_invalid axilio doctor", "CHECK          | STATUS | DETAIL\nAuthentication | ok     | method: api-key (source: env (AXILIO_API_KEY))\nCredentials    | ok     | API key present (axl_…)\nConnectivity   | ok     | https://api.axilio.ai/api/v1 reachable\nAuthentication | fail   | API key rejected (HTTP 401); run `axilio login`\n...", "doctor: API key rejected (HTTP 401); run `axilio login`", 3, "Missing or rejected credentials exit 3. When credentials are configured but the Axilio API cannot be reached, doctor exits 6."),
	}},
	"config": {Samples: []CommandSample{
		sample("axilio config", "API host     https://api.axilio.ai\nAuth method  api-key (source: config)\nActive org   (session default)\nConfig file  <config-path>\nSessions dir <sessions-directory>", "none"),
		sample("axilio config set base-url https://api.axilio.ai", "none", "Set base-url = https://api.axilio.ai in <config-path>"),
		sample("axilio config unset base-url", "none", "Unset base-url in <config-path>"),
	}},
	"config set": {Samples: []CommandSample{
		sample("axilio config set base-url https://api.axilio.ai", "none", "Set base-url = https://api.axilio.ai in <config-path>"),
	}},
	"config unset": {Samples: []CommandSample{
		sample("axilio config unset base-url", "none", "Unset base-url in <config-path>"),
	}},
	"orgs": {Samples: []CommandSample{
		sample("axilio orgs", "   SLUG         NAME          ID\n*  example-org  Example Inc.  <organization-id>", "none"),
		sample("axilio orgs list", "   SLUG         NAME          ID\n*  example-org  Example Inc.  <organization-id>", "none"),
		sample("axilio orgs use example-org", "none", "Active organization set to example-org (Example Inc.)."),
		sample("axilio --org another-org workflows list", "WORKFLOW ID   NAME      PLATFORM  STATUS  LAST RUN\n<workflow-id> Checkout  android   active  <timestamp>", "none"),
		sample("axilio orgs clear", "none", "Cleared the active organization; using your session default."),
	}},
	"orgs list": {Samples: []CommandSample{
		sample("axilio orgs list", "   SLUG         NAME          ID\n*  example-org  Example Inc.  <organization-id>", "none"),
		sample("axilio orgs list", "   SLUG         NAME          ID\n   example-org  Example Inc.  <organization-id>", "No active org set; using your session default. Set one with `axilio orgs use <slug>`."),
		sampleWithNote("axilio orgs list -o json", "{\n  \"active\": \"example-org\",\n  \"organizations\": [\n    {\"id\": \"<organization-id>\", \"slug\": \"example-org\", \"name\": \"Example Inc.\"}\n  ]\n}", "none", "Organization membership and the active selector vary by session."),
	}},
	"orgs use": {Samples: []CommandSample{
		sample("axilio orgs use example-org", "none", "Active organization set to example-org (Example Inc.)."),
	}},
	"orgs clear": {Samples: []CommandSample{
		sampleWithNote("axilio orgs clear", "none", "Cleared the active organization; using your session default.", "If no organization is set, it reports that state and still exits 0."),
	}},
	"upgrade": {Samples: []CommandSample{
		sampleWithNote("axilio upgrade --check", "none", "A newer release is available: <old> -> <new>. Run `axilio upgrade` to install.", "The version placeholders vary. This is the newer-release branch; up-to-date, no-release, development, and Homebrew-managed installations report their own state instead."),
		sampleWithNote("axilio upgrade", "none", "Upgrading axilio <current-version> -> <latest-version>...\nUpgraded to <latest-version>.", "Standalone release only; development builds and Homebrew-managed installs print their own package-manager guidance."),
		externalSample("brew upgrade axilio", "Output and exit status are owned by Homebrew, not the axilio CLI."),
	}},
	"init": {Samples: []CommandSample{
		sampleWithNote("axilio init", "none", "✓ Wrote the Axilio agent skill to <detected-target>\nNot signed in. A human must run `axilio login` (browser), or set AXILIO_API_KEY.", "Representative stderr excerpt; detected targets depend on repository markers, existing auto-detected skills may be skipped, and the command finishes with a suggested agent prompt."),
		sampleWithNote("axilio init --agent codex", "none", "✓ Wrote the Axilio agent skill to AGENTS.md\nNot signed in. A human must run `axilio login` (browser), or set AXILIO_API_KEY.", "Excerpt: stderr then prints the suggested first agent prompt."),
		sampleWithNote("axilio init --agent claude --force", "none", "✓ Wrote the Axilio agent skill to .claude/skills/axilio/SKILL.md\nNot signed in. A human must run `axilio login` (browser), or set AXILIO_API_KEY.", "Excerpt: --force replaces an existing Axilio-owned Claude skill before the suggested first agent prompt."),
	}},
	"sessions": workflow(
		"axilio sessions list",
		"axilio sessions start",
		"axilio sessions current",
		"axilio sessions stop sess_123",
	),
	"sessions list": {Samples: []CommandSample{
		sampleWithNote("axilio sessions list", "   SESSION       PHONE       TYPE\n*  <session-id>  <phone-id>  android", "none", "With no sessions saved locally, stdout explains how to start one and exit status is 0."),
		sampleWithNote("axilio sessions list --remote", "SESSION       PHONE       TYPE     MODEL\n<session-id>  <phone-id>  android  Pixel 8", "none", "Remote results come from the API and do not mark the session selected by this CLI."),
		sampleWithNote("axilio sessions list -o json", "[\n  {\n    \"session_id\": \"<session-id>\",\n    \"phone_id\": \"<phone-id>\",\n    \"phone_type\": \"android\",\n    \"control_url\": \"<control-url>\",\n    \"created_at\": \"<timestamp>\"\n  }\n]", "none", "Locally saved session JSON includes the stored control URL; IDs and URLs vary."),
	}},
	"sessions current": {Samples: []CommandSample{
		sampleWithNote("axilio sessions current", "Session  <session-id>\nPhone    <phone-id>\nType     android", "none", "No selected session is an exit-0 answer; JSON prints null."),
		sampleWithNote("AXILIO_SESSION=sess_123 axilio sessions current", "Session  sess_123\nPhone    <phone-id>\nType     android", "none", "The named session must be saved locally; AXILIO_SESSION takes precedence over automatic session selection."),
		sampleWithNote("axilio sessions current -o json", "{\n  \"session_id\": \"<session-id>\",\n  \"phone_id\": \"<phone-id>\",\n  \"phone_type\": \"android\",\n  \"control_url\": \"<control-url>\",\n  \"created_at\": \"<timestamp>\"\n}", "none", "With no selected session, the successful JSON result is null."),
	}},
	"sessions start": {Samples: []CommandSample{
		sample("axilio sessions start", "Session      <session-id>\nPhone        <phone-id>\nRegion       us-central\nLive view    <live-view-url>\nControl URL  <control-url>", "Drive it:  axilio phone observe\nPin it to this shell (for parallel work):  export AXILIO_SESSION=<session-id>\nRelease it with:  axilio sessions stop <session-id>"),
		sample("axilio sessions start --phone-type iphone", "Session      <session-id>\nPhone        <phone-id>\nRegion       us-central\nLive view    <live-view-url>\nControl URL  <control-url>", "Drive it:  axilio phone observe\nPin it to this shell (for parallel work):  export AXILIO_SESSION=<session-id>\nRelease it with:  axilio sessions stop <session-id>"),
		sample("axilio sessions start --phone-id ph_123", "Session      <session-id>\nPhone        ph_123\nRegion       us-central\nLive view    <live-view-url>\nControl URL  <control-url>", "Drive it:  axilio phone observe\nPin it to this shell (for parallel work):  export AXILIO_SESSION=<session-id>\nRelease it with:  axilio sessions stop <session-id>"),
		sample("axilio sessions start --workflow wf_123", "Session      <session-id>\nPhone        <phone-id>\nRegion       us-central\nLive view    <live-view-url>\nControl URL  <control-url>", "Drive it:  axilio phone observe\nPin it to this shell (for parallel work):  export AXILIO_SESSION=<session-id>\nRelease it with:  axilio sessions stop <session-id>"),
		sampleWithNote("axilio sessions start --export", "export AXILIO_SESSION=<session-id>", "none", "--export emits shell text even when -o json is also supplied."),
		sampleWithNote(`eval "$(axilio sessions start --export)"`, "none", "none", "The shell consumes the nested command's export line and sets AXILIO_SESSION in the current shell."),
	}},
	"sessions stop": {Samples: []CommandSample{
		sampleWithNote("axilio sessions stop sess_123", "none", "Release <phone-id>? [y/N] Released <phone-id>.", "Without --yes, table mode reads confirmation from stdin, including redirected input. JSON and quiet modes require --yes."),
		sample("axilio sessions stop ph_123 --yes", "none", "Released ph_123."),
	}},
	"phones": workflow("axilio phones list", "axilio phones mine"),
	"phones list": {Samples: []CommandSample{
		sampleWithNote("axilio phones list", "PHONE ID   TYPE     MODEL    STATUS\n<phone-id> android  Pixel 8  active", "none", "An empty successful result prints No phones available."),
		sampleWithNote("axilio phones list -o json", "{\n  \"android_count\": 1,\n  \"iphone_count\": 0,\n  \"phones\": [\n    {\n      \"created_at\": \"<timestamp>\",\n      \"model_name\": \"Pixel 8\",\n      \"ownership_type\": \"shared\",\n      \"phone_id\": \"<phone-id>\",\n      \"phone_type\": \"android\",\n      \"status\": \"active\",\n      \"updated_at\": \"<timestamp>\"\n    }\n  ]\n}", "none", "Counts, inventory, and optional phone fields vary by organization and availability."),
	}},
	"phones mine": {Samples: []CommandSample{
		sampleWithNote("axilio phones mine", "PHONE ID   NICKNAME  TYPE     MODEL    STATUS  SESSION\n<phone-id> demo      android  Pixel 8  active  <session-id>", "none", "An empty successful result prints No dedicated phones."),
	}},
	"phone": workflow(
		"axilio phone observe",
		`axilio phone tap --query "the search box"`,
		`axilio phone type "Axilio"`,
		"axilio phone observe",
	),
	"phone observe": {
		Samples: []CommandSample{
			sampleWithNote("axilio phone observe", "TEXT          X    Y     CONF\ncoffee shops  540  620   0.99\nContinue      540  1120  0.98", "2 texts, 0 icons  1080x2400", "JSON includes texts, icons, dimensions, screen hash, and capture time."),
			sampleWithNote("axilio phone observe --ocr-engine premium", "TEXT          X    Y     CONF\ncoffee shops  540  620   0.99\nContinue      540  1120  0.98", "2 texts, 0 icons  1080x2400", "Premium OCR changes recognition, not the table shape."),
			sampleWithNote("axilio phone observe -o json", "{\n  \"texts\": [\n    {\"text\": \"Continue\", \"center\": {\"x\": 540, \"y\": 1120}, \"confidence\": 0.98}\n  ],\n  \"icons\": [],\n  \"hash\": \"<screen-hash>\",\n  \"width\": 1080,\n  \"height\": 2400,\n  \"captured_at\": \"<timestamp>\"\n}", "none", "Shortened representative screen JSON; elements also include bounding boxes and sources."),
		},
		Walkthrough: phoneObserveWalkthrough,
	},
	"phone find": {Samples: []CommandSample{
		sample(`axilio phone find "the search box"`, "Text        Search\nCenter      540,620\nBBox        80,560 920x120\nConfidence  0.99\nSource      ocr", "none"),
		sampleWithNote(`axilio phone find "settings icon" --ocr-engine premium`, "Text\nCenter      900,120\nBBox        840,60 120x120\nConfidence  0.98\nSource      vlm", "none", "Vision-model elements can have no text; coordinates depend on the current frame."),
		sampleWithNote(`axilio phone find "continue button" --timeout 15s -o json`, "{\n  \"text\": \"Continue\",\n  \"center\": {\"x\": 540, \"y\": 1120},\n  \"bbox\": {\"x\": 80, \"y\": 1060, \"width\": 920, \"height\": 120},\n  \"confidence\": 0.98,\n  \"source\": \"ocr\"\n}", "none", "Element content and coordinates depend on the current frame."),
	}},
	"phone find-text": {Samples: []CommandSample{
		sampleWithNote(`axilio phone find-text "sign in"`, "Text        Sign in\nCenter      540,1120\nBBox        80,1060 920x120\nConfidence  0.98\nSource      ocr", "none", "The default is a case-insensitive substring match."),
		sampleWithNote(`axilio phone find-text "Sign in" --exact -o json`, "{\n  \"bbox\": {\n    \"x\": 80,\n    \"y\": 1060,\n    \"width\": 920,\n    \"height\": 120\n  },\n  \"center\": {\n    \"x\": 540,\n    \"y\": 1120\n  },\n  \"confidence\": 0.98,\n  \"text\": \"Sign in\",\n  \"source\": \"ocr\"\n}", "none", "Exact matching is case-sensitive."),
		sampleWithNote(`axilio phone find-text "settings"`, "No match.", "none", "No match is success; -o json emits null."),
		sampleWithNote(`axilio phone find-text "settings" -o json`, "null", "none", "No match is a successful literal JSON null."),
	}},
	"phone tap": {Samples: []CommandSample{
		sample(`axilio phone tap --query "the search box"`, "none", `Tapped "the search box" at 540,620`),
		sample("axilio phone tap 540 1200", "none", "Tapped 540,1200"),
		sample(`axilio phone tap --session sess_123 --query "continue"`, "none", `Tapped "continue" at 540,1120`),
	}},
	"phone long-press": {Samples: []CommandSample{
		sample("axilio phone long-press 540 1080", "none", "Long-pressed 540,1080 for 800ms"),
		sample("axilio phone long-press 540 1080 --duration-ms 1200", "none", "Long-pressed 540,1080 for 1200ms"),
	}},
	"phone swipe": {Samples: []CommandSample{
		sample("axilio phone swipe 540 1600 540 500", "none", "Swiped 540,1600 -> 540,500"),
		sample("axilio phone swipe 200 800 900 800 --duration-ms 500", "none", "Swiped 200,800 -> 900,800"),
	}},
	"phone type": {Samples: []CommandSample{
		sample(`axilio phone type "hello world"`, "none", `Typed "hello world"`),
		sample(`axilio phone type 'user@example.com'`, "none", `Typed "user@example.com"`),
		sample(`axilio phone type "don't split this text"`, "none", `Typed "don't split this text"`),
	}},
	"phone key": {Samples: []CommandSample{
		sample("axilio phone key enter", "none", "Pressed enter"),
	}},
	"phone screenshot": {Samples: []CommandSample{
		sampleWithNote("axilio phone screenshot", "none", "Wrote screenshot.png (<bytes> bytes)", "A new file requests mode 0644, subject to the process umask. Overwriting an existing file preserves its mode while replacing its contents."),
		sampleWithNote("axilio phone screenshot --out login.png", "none", "Wrote login.png (<bytes> bytes)", "A new file requests mode 0644, subject to the process umask. Overwriting an existing file preserves its mode while replacing its contents."),
	}},
	"phone wait-for": {Samples: []CommandSample{
		sample(`axilio phone wait-for "Results"`, "Text        Results\nCenter      540,1120\nBBox        80,1060 920x120\nConfidence  0.98\nSource      ocr", "none"),
		sample(`axilio phone wait-for "Loading" --gone`, "none", `"Loading" gone`),
		sampleWithNote(`axilio phone wait-for "Ready" --exact --timeout 30s`, "Text        Ready\nCenter      540,1120\nBBox        80,1060 920x120\nConfidence  0.98\nSource      ocr", "none", "Exact matching is case-sensitive and waits up to 30 seconds."),
		failedSample(`axilio phone wait-for "Results" --timeout 1s`, "none", "timeout: text not found within deadline: Results", 5, "A wait timeout exits 5."),
	}},
	"phone send": {Samples: []CommandSample{
		sampleWithNote("axilio phone send ./photo.jpg", "Delivery  <delivery-id>\nFile      photo.jpg\nStatus    dispatched", "→ Sending photo.jpg to phone <phone-id>\npushed without requesting delivery receipt. In the future, add --wait if you want the cli to wait for delivery confirmation and report result.", "The upload remains in the organization library."),
		sampleWithNote("axilio phone send ./clip.mp4 --collection Movies", "Delivery  <delivery-id>\nFile      clip.mp4\nStatus    dispatched", "→ Sending clip.mp4 to phone <phone-id>\npushed without requesting delivery receipt. In the future, add --wait if you want the cli to wait for delivery confirmation and report result.", "The upload remains in the organization library and is delivered to Movies."),
		sampleWithNote("axilio phone send ./photo.jpg --wait --timeout 2m", "Delivery  <delivery-id>\nFile      photo.jpg\nStatus    delivered", "→ Sending photo.jpg to phone <phone-id>", "With --wait, the command returns only after delivered, failed, or the two-minute deadline."),
	}},
	"workflows": workflow(
		"axilio workflows list",
		"axilio workflows list --search checkout",
		"axilio runs start wf_123",
	),
	"workflows list": {Samples: []CommandSample{
		sampleWithNote("axilio workflows list", "WORKFLOW ID   NAME      PLATFORM  STATUS  LAST RUN\n<workflow-id> Checkout  android   active  <timestamp>", "none", "An empty successful result prints No workflows found."),
		sampleWithNote("axilio workflows list --search checkout --limit 10", "WORKFLOW ID   NAME      PLATFORM  STATUS  LAST RUN\n<workflow-id> Checkout  android   active  <timestamp>", "none", "Names, IDs, statuses, and timestamps vary by organization."),
		sampleWithNote("axilio workflows list -o json", "{\n  \"limit\": 20,\n  \"offset\": 0,\n  \"total\": 1,\n  \"workflows\": [\n    {\n      \"workflow\": {\n        \"id\": \"<workflow-id>\",\n        \"name\": \"Checkout\",\n        \"platform\": \"android\",\n        \"status\": \"active\"\n      }\n    }\n  ]\n}", "none", "Shortened representative JSON output; workflow records may include additional fields and statistics."),
	}},
	"runs": workflow(
		"axilio workflows list",
		"axilio runs start wf_123",
		"axilio runs list --workflow wf_123",
		"axilio runs get run_123",
	),
	"runs list": {Samples: []CommandSample{
		sampleWithNote("axilio runs list", "RUN ID   STATUS     TRIGGER  WORKFLOW      CREATED\n<run-id> completed  manual   <workflow-id> <timestamp>", "none", "An empty successful result prints No runs found."),
		sampleWithNote("axilio runs list --workflow wf_123 --limit 10", "RUN ID   STATUS     TRIGGER  WORKFLOW  CREATED\n<run-id> completed  manual   wf_123    <timestamp>", "none", "Run IDs, statuses, and timestamps vary."),
		sampleWithNote("axilio runs list -o json", "{\n  \"limit\": 20,\n  \"offset\": 0,\n  \"runs\": [\n    {\n      \"id\": \"<run-id>\",\n      \"status\": \"completed\",\n      \"trigger\": \"manual\",\n      \"workflow_id\": \"<workflow-id>\"\n    }\n  ],\n  \"total\": 1\n}", "none", "Shortened representative JSON output; run records may include additional fields."),
	}},
	"runs start": {Samples: []CommandSample{
		sampleWithNote("axilio runs start wf_123", "none", "Started run <run-id>", "-o json emits the complete run_ids response on stdout."),
		sample("axilio runs start wf_123 --count 3", "none", "Started run <run-id-1>\nStarted run <run-id-2>\nStarted run <run-id-3>"),
		sample("axilio runs start wf_123 --phone-id ph_123", "none", "Started run <run-id>"),
		sample("axilio runs start wf_123 --start-timeout 300", "none", "Started run <run-id>"),
	}},
	"runs get": {Samples: []CommandSample{
		sample("axilio runs get run_123", "Run        run_123\nStatus     completed\nTrigger    manual\nWorkflow   <workflow-id>\nSession    <session-id>\nPhone      <phone-id>\nCreated    <created-at>\nStarted    <started-at>\nCompleted  <completed-at>\nError      -\nVideo      <video-url>", "none"),
		sampleWithNote("axilio runs get run_123 -o json", "{\n  \"id\": \"run_123\",\n  \"status\": \"completed\",\n  \"trigger\": \"manual\",\n  \"workflow_id\": \"<workflow-id>\",\n  \"session_id\": \"<session-id>\",\n  \"phone_id\": \"<phone-id>\"\n}", "none", "Shortened representative JSON output; timestamps, errors, and video URL appear when available."),
	}},
	"runs cancel": {Samples: []CommandSample{
		sampleWithNote("axilio runs cancel run_123", "none", "Cancel run run_123? [y/N] Canceled run_123", "Without --yes, table mode reads confirmation from stdin, including redirected input. JSON and quiet modes require --yes."),
		sample("axilio runs cancel run_123 --yes", "none", "Canceled run_123"),
	}},
	"api-keys": workflow(
		"axilio api-keys list",
		"axilio api-keys create ci",
		"axilio api-keys delete key_123 --yes",
	),
	"api-keys list": {Samples: []CommandSample{
		sampleWithNote("axilio api-keys list", "ID       NAME  KEY       LAST USED  CREATED\n<key-id> ci    axl_ci…   -          <timestamp>", "none", "Only the masked preview is returned; an empty result says no keys were found."),
		sampleWithNote("axilio api-keys list -o json", "{\n  \"api_keys\": [\n    {\n      \"id\": \"<key-id>\",\n      \"name\": \"ci\",\n      \"key_preview\": \"axl_ci…\",\n      \"created_at\": \"<timestamp>\"\n    }\n  ],\n  \"limit\": 50,\n  \"offset\": 0,\n  \"total\": 1\n}", "none", "Key IDs and timestamps vary; full secrets are never listed."),
	}},
	"api-keys create": {Samples: []CommandSample{
		sample("axilio api-keys create ci", "ID       <key-id>\nName     ci\nKey      axl_<new-secret>\nCreated  <timestamp>", "Save this key now; it will not be shown again."),
		sample(`axilio api-keys create "release automation" -o json`, "{\n  \"id\": \"<key-id>\",\n  \"name\": \"release automation\",\n  \"key_value\": \"axl_<new-secret>\",\n  \"created_at\": \"<timestamp>\"\n}", "none"),
	}},
	"api-keys delete": {Samples: []CommandSample{
		sampleWithNote("axilio api-keys delete key_123", "none", "Delete API key key_123? [y/N] Deleted key_123", "Without --yes, table mode reads confirmation from stdin, including redirected input. JSON and quiet modes require --yes."),
		sample("axilio api-keys delete key_123 --yes", "none", "Deleted key_123"),
	}},
	"uploads": workflow(
		"axilio uploads list",
		"axilio uploads add ./photo.jpg",
		"axilio uploads push upl_123 --phone-id ph_123",
		"axilio uploads delete upl_123 --yes",
	),
	"uploads add": {Samples: []CommandSample{
		sample("axilio uploads add ./photo.jpg", "ID        <upload-id>\nFilename  photo.jpg\nSize      2.0 MiB\nType      image/jpeg\nStatus    ready", "→ Uploading photo.jpg"),
		sampleWithNote("axilio uploads add ./asset --filename photo.jpg --mime-type image/jpeg", "ID        <upload-id>\nFilename  photo.jpg\nSize      2.0 MiB\nType      image/jpeg\nStatus    ready", "→ Uploading asset", "--filename and --mime-type override values inferred from the local path."),
	}},
	"uploads list": {Samples: []CommandSample{
		sampleWithNote("axilio uploads list", "ID          FILENAME   SIZE     TYPE        STATUS  CREATED\n<upload-id> photo.jpg  2.0 MiB  image/jpeg  ready   <timestamp>", "1 of 10000 files, 2.0 MiB of 50.0 GiB used", "An empty page still reports quota usage; the current global limits are 10000 files and 50 GiB."),
		sampleWithNote("axilio uploads list --search receipt --limit 20 --offset 0", "ID          FILENAME     SIZE      TYPE       STATUS  CREATED\n<upload-id> receipt.jpg  512.0 KiB image/jpeg ready   <timestamp>", "1 of 10000 files, 512.0 KiB of 50.0 GiB used", "Results and bytes used vary by organization; the limits are global."),
		sampleWithNote("axilio uploads list --sort filename --order asc", "ID          FILENAME   SIZE     TYPE        STATUS  CREATED\n<upload-id> photo.jpg  2.0 MiB  image/jpeg  ready   <timestamp>", "1 of 10000 files, 2.0 MiB of 50.0 GiB used", "Rows are sorted by filename ascending; results and bytes used vary by organization."),
		sampleWithNote("axilio uploads list -o json", "{\n  \"files\": [\n    {\n      \"id\": \"<upload-id>\",\n      \"filename\": \"photo.jpg\",\n      \"size_bytes\": 2097152,\n      \"mime_type\": \"image/jpeg\",\n      \"status\": \"ready\",\n      \"created_at\": \"<timestamp>\"\n    }\n  ],\n  \"total\": 1,\n  \"usage\": {\n    \"file_count\": 1,\n    \"file_limit\": 10000,\n    \"total_bytes\": 2097152,\n    \"byte_limit\": 53687091200\n  }\n}", "none", "Representative response; files, result count, and bytes used vary by organization; limits are global."),
	}},
	"uploads push": {Samples: []CommandSample{
		sample("axilio uploads push upl_123 --phone-id ph_123", "Delivery  <delivery-id>\nFile      photo.jpg\nStatus    dispatched", "→ Pushing upl_123 to phone ph_123\npushed without requesting delivery receipt. In the future, add --wait if you want the cli to wait for delivery confirmation and report result."),
		sampleWithNote("axilio uploads push upl_123 --phone-id ph_123 --collection Pictures", "Delivery  <delivery-id>\nFile      photo.jpg\nStatus    dispatched", "→ Pushing upl_123 to phone ph_123\npushed without requesting delivery receipt. In the future, add --wait if you want the cli to wait for delivery confirmation and report result.", "The phone receives the file in Pictures."),
		sampleWithNote("axilio uploads push upl_123 --phone-id ph_123 --wait --timeout 2m", "Delivery  <delivery-id>\nFile      photo.jpg\nStatus    delivered", "→ Pushing upl_123 to phone ph_123", "With --wait, the command returns only after delivered, failed, or the two-minute deadline."),
	}},
	"uploads delete": {Samples: []CommandSample{
		sampleWithNote("axilio uploads delete upl_123", "none", "Delete upload upl_123? Also recall it from phones holding or receiving a copy?", "The table-mode prompt continues with [y/N], then reports Deleted upl_123 after confirmation. Redirected stdin can answer; JSON and quiet modes require --yes."),
		sample("axilio uploads rm upl_123 --yes", "none", "Deleted upl_123"),
	}},
}

func attachGeneratedCommandDocumentation(root *cobra.Command) {
	longDescriptions := map[string]string{
		"completion": `Generate a shell completion script for axilio.

After installing the generated completion script, users can type:

  axilio api-<Tab>
  axilio phone <Tab>
  axilio sessions start --<Tab>

Their shell can complete command names, subcommands, and flags, reducing typos and making the command tree easier to discover. Completion is currently limited to this static command metadata; Axilio resource IDs and names are not fetched dynamically.

Choose bash, zsh, fish, or powershell. The generated script is written to stdout so it can be sourced for the current shell or redirected to that shell's completion directory.`,
		"completion bash": `Generate the Bash completion script for axilio.

Load completions in the current shell:

  source <(axilio completion bash)

For future shells, install the script in Bash's completion directory:

  axilio completion bash > /etc/bash_completion.d/axilio

On macOS with Homebrew's bash-completion, use:

  axilio completion bash > "$(brew --prefix)/etc/bash_completion.d/axilio"

The bash-completion package must be installed and loaded.`,
		"completion zsh": `Generate the Zsh completion script for axilio.

Enable Zsh completion if needed, then install the script in a directory on fpath:

  autoload -U compinit; compinit
  axilio completion zsh > "${fpath[1]}/_axilio"

On macOS with Homebrew, use:

  axilio completion zsh > "$(brew --prefix)/share/zsh/site-functions/_axilio"

Start a new shell after installing the script.`,
		"completion fish": `Generate the Fish completion script for axilio.

Install it for future shells:

  axilio completion fish > ~/.config/fish/completions/axilio.fish

Fish loads completion files from this directory automatically.`,
		"completion powershell": `Generate the PowerShell completion script for axilio.

Load completions in the current shell:

  axilio completion powershell | Out-String | Invoke-Expression

Add the command to your PowerShell profile to load completions in future shells.`,
	}
	generated := map[string]CommandDocumentation{
		"help": {Samples: []CommandSample{
			sampleWithNote("axilio help phone tap", "Perform a tap action on the selected phone.\n\nUSAGE\n\n  axilio phone tap [x y] [--flags]", "none", "The complete help includes examples and command-effective flag descriptions."),
			sampleWithNote("axilio help --html", "file:///path/to/axilio.1.html", "none", "The URL points to the self-contained HTML manual installed with Homebrew or install.sh."),
		}},
		"completion": workflow(
			"source <(axilio completion bash)",
			`axilio completion zsh > "${fpath[1]}/_axilio"`,
			"axilio completion fish > ~/.config/fish/completions/axilio.fish",
			"axilio completion powershell | Out-String | Invoke-Expression",
		),
		"completion bash": {Samples: []CommandSample{
			sampleWithNote("source <(axilio completion bash)", "none", "none", "Loads completions in the current Bash session."),
		}},
		"completion zsh": {Samples: []CommandSample{
			sampleWithNote(`axilio completion zsh > "${fpath[1]}/_axilio"`, "none", "none", "Installs the script for future Zsh sessions; start a new shell after installation."),
		}},
		"completion fish": {Samples: []CommandSample{
			sampleWithNote("axilio completion fish > ~/.config/fish/completions/axilio.fish", "none", "none", "Installs the script where Fish loads it automatically."),
		}},
		"completion powershell": {Samples: []CommandSample{
			sampleWithNote("axilio completion powershell | Out-String | Invoke-Expression", "none", "none", "Loads completions in the current PowerShell session."),
		}},
	}
	for path, docs := range generated {
		command := root
		for _, name := range strings.Fields(path) {
			var next *cobra.Command
			for _, child := range command.Commands() {
				if child.Name() == name {
					next = child
					break
				}
			}
			command = next
			if command == nil {
				break
			}
		}
		if command == nil || command.CommandPath() != root.Name()+" "+path {
			panic("missing generated command documentation target " + path)
		}
		if description, ok := longDescriptions[path]; ok {
			command.Long = description
		}
		AttachCommandDocumentation(command, docs)
	}
}
