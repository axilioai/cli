package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// commandGlobalFlagHelp describes what each inherited root flag actually does
// when a particular command runs. Root flags are shared pflag objects, so the
// help renderer applies these descriptions temporarily and restores them after
// rendering. Runtime parsing and behavior remain unchanged.
type commandGlobalFlagHelp struct {
	apiKey  string
	baseURL string
	noColor string
	org     string
	output  string
	quiet   string
}

func (help commandGlobalFlagHelp) usage(name string) (string, bool) {
	switch name {
	case "api-key":
		return help.apiKey, help.apiKey != ""
	case "base-url":
		return help.baseURL, help.baseURL != ""
	case "no-color":
		return help.noColor, help.noColor != ""
	case "org":
		return help.org, help.org != ""
	case "output":
		return help.output, help.output != ""
	case "quiet":
		return help.quiet, help.quiet != ""
	default:
		return "", false
	}
}

func commandGlobalFlagUsage(command *cobra.Command, name string) (string, bool) {
	help, ok := globalFlagHelpByCommand[commandHelpKey(command)]
	if !ok {
		return "", false
	}
	return help.usage(name)
}

func commandOwnedFlagUsage(command *cobra.Command, name string) (string, bool) {
	if commandHelpKey(command) == "phone" && name == "session" {
		return "No effect on phone command; bare axilio phone only displays this help", true
	}
	return "", false
}

func commandHelpKey(command *cobra.Command) string {
	return strings.TrimPrefix(command.CommandPath(), command.Root().Name()+" ")
}

const (
	apiKeyPrecedence  = "overrides AXILIO_API_KEY and the saved key"
	baseURLPrecedence = "overrides AXILIO_BASE_URL and saved base-url"
	orgPrecedence     = "overrides AXILIO_ORG and the saved active org; API keys ignore it"
	phoneControlAuth  = "No effect; the session's embedded control token in the websocket URL authenticates phone commands"
)

func apiResultHelp(action, result string) commandGlobalFlagHelp {
	return commandGlobalFlagHelp{
		apiKey:  fmt.Sprintf("API key for %s; %s", action, apiKeyPrecedence),
		baseURL: fmt.Sprintf("API host for %s; %s", action, baseURLPrecedence),
		noColor: fmt.Sprintf("Disable ANSI color in the human-readable form of %s", result),
		org:     fmt.Sprintf("OAuth org for %s; %s", action, orgPrecedence),
		output:  fmt.Sprintf("Render %s as table or json", result),
		quiet:   fmt.Sprintf("Suppress stderr notes for %s; %s remains on stdout", action, result),
	}
}

func apiActionHelp(action string, destructive bool) commandGlobalFlagHelp {
	quiet := fmt.Sprintf("Suppress the human %s result; JSON still prints", action)
	if destructive {
		quiet = fmt.Sprintf("Suppress the %s prompt and human result; --yes is still required; JSON still prints", action)
	}
	return commandGlobalFlagHelp{
		apiKey:  fmt.Sprintf("API key for %s; %s", action, apiKeyPrecedence),
		baseURL: fmt.Sprintf("API host for %s; %s", action, baseURLPrecedence),
		noColor: fmt.Sprintf("No effect on %s; its runtime messages are unstyled", action),
		org:     fmt.Sprintf("OAuth org for %s; %s", action, orgPrecedence),
		output:  fmt.Sprintf("Emit a human confirmation or JSON %s result", action),
		quiet:   quiet,
	}
}

func localResultHelp(command, action, result string) commandGlobalFlagHelp {
	return commandGlobalFlagHelp{
		apiKey:  fmt.Sprintf("No effect on %s command; it uses local session data or its control URL", command),
		baseURL: fmt.Sprintf("No effect on %s command; it does not call the Axilio API", command),
		noColor: fmt.Sprintf("Disable ANSI color in the human-readable form of %s", result),
		org:     fmt.Sprintf("No effect on %s command; phone control is selected by session, not org", command),
		output:  fmt.Sprintf("Render %s as table or json", result),
		quiet:   fmt.Sprintf("Suppress stderr notes for %s; %s remains on stdout", action, result),
	}
}

func phoneResultHelp(command, action, result string) commandGlobalFlagHelp {
	help := localResultHelp(command, action, result)
	help.apiKey = phoneControlAuth
	return help
}

func localActionHelp(command, action, outcome string) commandGlobalFlagHelp {
	return commandGlobalFlagHelp{
		apiKey:  phoneControlAuth,
		baseURL: fmt.Sprintf("No effect on %s command; it does not call the Axilio API", command),
		noColor: fmt.Sprintf("No effect on %s command; its runtime message is unstyled", command),
		org:     fmt.Sprintf("No effect on %s command; the target is selected by session, not org", command),
		output:  fmt.Sprintf("Emit a human confirmation or JSON %s result", outcome),
		quiet:   fmt.Sprintf("Suppress the human %s result; the phone action still runs; JSON still prints", action),
	}
}

func noEffectHelp(command string) commandGlobalFlagHelp {
	message := fmt.Sprintf("No effect on %s command", command)
	return commandGlobalFlagHelp{
		apiKey:  message,
		baseURL: message,
		noColor: message,
		org:     message,
		output:  message,
		quiet:   message,
	}
}

func helpOnlyCommandHelp(command string) commandGlobalFlagHelp {
	message := fmt.Sprintf("No effect on %s command; bare axilio %s only displays this help", command, command)
	return commandGlobalFlagHelp{
		apiKey:  message,
		baseURL: message,
		noColor: message,
		org:     message,
		output:  message,
		quiet:   message,
	}
}

func buildGlobalFlagHelp() map[string]commandGlobalFlagHelp {
	help := map[string]commandGlobalFlagHelp{
		"login": {
			apiKey:  "Verify and save this API key instead of opening browser OAuth",
			baseURL: "API host for login; also saved when API-key login succeeds",
			noColor: "Disable ANSI color in login progress and success messages",
			org:     "Does not choose the login org; only scopes the post-login OAuth balance request",
			output:  "Emit human login messages or a JSON sign-in result",
			quiet:   "Suppress human login messages; browser OAuth still opens; JSON still prints",
		},
		"logout": {
			apiKey:  "No effect on logout command; saved credentials are cleared regardless",
			baseURL: "No effect on logout command; OAuth revocation uses the session's saved host",
			noColor: "Disable ANSI color in the signed-out success message",
			org:     "No effect on logout command; the saved active org is cleared regardless",
			output:  "Emit a human sign-out message or JSON sign-out result",
			quiet:   "Suppress human logout warnings and results; JSON still prints",
		},
		"config": {
			apiKey: "No actual effect. Ephemerally shows this as the effective API key source;\n" +
				"does not persist the new key. Use axilio login --api-key [api_key] to\n" +
				"verify and persist a new key.",
			baseURL: "No actual effect. Ephemerally shows this as the effective API host;\n" +
				"does not save the new host. See the above help content to change base-url.",
			noColor: "Disable ANSI color in the human-readable configuration table",
			org: "No actual effect. Ephemerally changes the displayed org only, does not save the selection.\n" +
				"Use axilio orgs use [org_name] instead to persist changes to active org setting.",
			output: "Render the configuration summary as table or json",
			quiet:  "Suppress stderr update notices; the configuration summary remains on stdout",
		},
		"config set": {
			apiKey:  "No effect on config set command; only the named config value is saved",
			baseURL: "No effect on config set command; the positional value is saved instead",
			noColor: "No effect on config set command; its confirmation is unstyled",
			org:     "No effect on config set command; active org is not an editable key",
			output:  "Emit a human confirmation or JSON key, value, and config-path result",
			quiet:   "Suppress the human config set confirmation; JSON still prints",
		},
		"config unset": {
			apiKey:  "No effect on config unset command; credentials are not removed",
			baseURL: "No effect on config unset command; it removes the saved base-url",
			noColor: "No effect on config unset command; its confirmation is unstyled",
			org:     "No effect on config unset command; active org is not removed",
			output:  "Emit a human confirmation or JSON key, unset, and config-path result",
			quiet:   "Suppress the human config unset confirmation; JSON still prints",
		},
		"orgs":      orgListHelp("orgs"),
		"orgs list": orgListHelp("orgs list"),
		"orgs use": {
			apiKey:  "API-key auth cannot switch orgs; use a saved OAuth session",
			baseURL: fmt.Sprintf("API host for the membership check; %s", baseURLPrecedence),
			noColor: "No effect on orgs use command; its confirmation is unstyled",
			org:     "Does not choose the org to save; the positional slug or ID does",
			output:  "Emit a human confirmation or JSON active-organization result",
			quiet:   "Suppress the human org-selection confirmation; JSON still prints",
		},
		"orgs clear": {
			apiKey:  "No effect on orgs clear command; it only clears the saved org selection",
			baseURL: "No effect on orgs clear command; it makes no API request",
			noColor: "No effect on orgs clear command; its confirmation is unstyled",
			org:     "No effect on orgs clear command; the supplied override is not saved",
			output:  "Emit a human confirmation or JSON clear result",
			quiet:   "Suppress the human org-clear confirmation; JSON still prints",
		},
		"upgrade": {
			apiKey:  "No effect on upgrade command; release checks use GitHub without Axilio auth",
			baseURL: "No effect on upgrade command; release checks use GitHub",
			noColor: "No effect on upgrade command; its runtime messages are unstyled",
			org:     "No effect on upgrade command; releases are not organization-scoped",
			output:  "Emit human upgrade guidance or a JSON upgrade-status result",
			quiet:   "Suppress human upgrade guidance and status; JSON still prints",
		},
		"init": {
			apiKey:  "No effect on init command. A supplied value only changes the final sign-in message",
			baseURL: fmt.Sprintf("Host for the skill download and sign-in check; %s", baseURLPrecedence),
			noColor: "Disable ANSI color in init success messages",
			org:     "No effect on init command. Skill download and generated files are not org-scoped",
			output:  "Emit human init messages or JSON written and skipped path lists",
			quiet:   "Suppress human init prompts and messages; --agent may be required; JSON still prints",
		},
		"sessions":  helpOnlyCommandHelp("sessions"),
		"phones":    helpOnlyCommandHelp("phones"),
		"phone":     helpOnlyCommandHelp("phone"),
		"workflows": helpOnlyCommandHelp("workflows"),
		"runs":      helpOnlyCommandHelp("runs"),
		"api-keys":  helpOnlyCommandHelp("api-keys"),
		"uploads":   helpOnlyCommandHelp("uploads"),
		"help": {
			apiKey:  "No effect on help command or the help content it renders",
			baseURL: "No effect on help command or the help content it renders",
			noColor: "No effect on help command; help styling is controlled by the renderer",
			org:     "No effect on help command or the help content it renders",
			output:  "Does not reformat help as JSON; json only suppresses the update notice",
			quiet:   "Does not hide help; only suppresses the post-command update notice",
		},
		"completion": noEffectHelp("completion"),
	}

	help["status"] = apiResultHelp("the status request", "the status result")
	help["doctor"] = apiResultHelp("doctor's authenticated checks", "the doctor report")

	help["sessions list"] = commandGlobalFlagHelp{
		apiKey:  "Authenticates only --remote listing; no effect on local lease files",
		baseURL: "Selects the API host only with --remote; no effect on local lease files",
		noColor: "Disable ANSI color in the human-readable session listing",
		org:     "Selects the OAuth org only with --remote; no effect on local lease files",
		output:  "Render local or remote sessions as table or json",
		quiet:   "Suppress stderr notes; the session listing remains on stdout",
	}
	help["sessions current"] = localResultHelp("sessions current", "session resolution", "the current-session result")
	help["sessions start"] = apiResultHelp("the phone-allocation request", "the allocated-session result")
	help["sessions start"] = withOutput(help["sessions start"],
		"Render the allocated session as table or json; --export always emits shell text",
		"Suppress session guidance; the result or --export text remains on stdout")
	help["sessions stop"] = apiActionHelp("session release", true)

	help["phones list"] = apiResultHelp("the available-phone request", "the available-phone result")
	help["phones mine"] = apiResultHelp("the dedicated-phone request", "the dedicated-phone result")

	help["phone observe"] = phoneResultHelp("phone observe", "observation", "the observation result")
	help["phone find"] = phoneResultHelp("phone find", "vision search", "the found-element result")
	help["phone find-text"] = phoneResultHelp("phone find-text", "OCR search", "the text-match result")
	help["phone tap"] = localActionHelp("phone tap", "tap", "tap")
	help["phone long-press"] = localActionHelp("phone long-press", "long-press", "long-press")
	help["phone swipe"] = localActionHelp("phone swipe", "swipe", "swipe")
	help["phone type"] = localActionHelp("phone type", "typing", "typing")
	help["phone key"] = localActionHelp("phone key", "key press", "key press")
	help["phone screenshot"] = localActionHelp("phone screenshot", "screenshot", "screenshot write")
	help["phone wait-for"] = phoneResultHelp("phone wait-for", "OCR polling", "the matched-element result")
	help["phone wait-for"] = withOutput(help["phone wait-for"],
		"Render a present match or --gone result as table or json",
		"Suppress human wait notes; table or JSON results remain on stdout")
	help["phone send"] = apiResultHelp("the upload-and-delivery request", "the delivery result")
	help["phone send"] = withQuiet(help["phone send"],
		"Suppress upload progress and delivery notes; the delivery result remains on stdout")

	help["workflows list"] = apiResultHelp("the workflow-list request", "the workflow-list result")
	help["runs list"] = apiResultHelp("the run-list request", "the run-list result")
	help["runs start"] = apiResultHelp("the run-create request", "the created-run result")
	help["runs start"] = withOutput(help["runs start"],
		"Emit created run IDs as human messages or json",
		"Suppress table-mode run IDs and guidance; a JSON result remains on stdout")
	help["runs start"] = withNoColor(help["runs start"],
		"No effect on runs start command; its human messages are unstyled")
	help["runs get"] = apiResultHelp("the run-detail request", "the run-detail result")
	help["runs cancel"] = apiActionHelp("run cancellation", true)

	help["api-keys list"] = apiResultHelp("the API-key list request", "the API-key list")
	help["api-keys create"] = apiResultHelp("the API-key create request", "the created API key")
	help["api-keys create"] = withQuiet(help["api-keys create"],
		"Suppress the save-now warning; the created key remains on stdout")
	help["api-keys delete"] = apiActionHelp("API-key deletion", true)

	help["uploads add"] = apiResultHelp("the file-upload request", "the stored-upload result")
	help["uploads add"] = withQuiet(help["uploads add"],
		"Suppress upload progress; the stored-upload result remains on stdout")
	help["uploads list"] = apiResultHelp("the upload-list request", "the upload-list result")
	help["uploads push"] = apiResultHelp("the file-delivery request", "the delivery result")
	help["uploads push"] = withQuiet(help["uploads push"],
		"Suppress delivery progress and notes; the delivery result remains on stdout")
	help["uploads delete"] = apiResultHelp("the upload-delete request", "the deletion result")
	help["uploads delete"] = withOutput(help["uploads delete"],
		"Emit a human confirmation or JSON deletion result",
		"Suppress the prompt and human confirmation; --yes is required; JSON still prints")
	help["uploads delete"] = withNoColor(help["uploads delete"],
		"No effect on uploads delete command; its human confirmation is unstyled")

	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		key := "completion " + shell
		help[key] = commandGlobalFlagHelp{
			apiKey:  fmt.Sprintf("No effect on %s completion generation", shell),
			baseURL: fmt.Sprintf("No effect on %s completion generation", shell),
			noColor: fmt.Sprintf("No effect on %s completion; the script contains no ANSI styling", shell),
			org:     fmt.Sprintf("No effect on %s completion generation", shell),
			output:  fmt.Sprintf("Does not reformat %s completion as JSON; the shell script remains on stdout", shell),
			quiet:   fmt.Sprintf("Does not suppress the %s completion script; only stderr notices", shell),
		}
	}

	return help
}

func orgListHelp(command string) commandGlobalFlagHelp {
	return commandGlobalFlagHelp{
		apiKey:  "API-key auth cannot list OAuth memberships; use a saved OAuth session",
		baseURL: fmt.Sprintf("API host for the organization-list request; %s", baseURLPrecedence),
		noColor: "Disable ANSI color in the human-readable organization listing",
		org:     "No effect on orgs list command; the supplied override is not saved, only changes displayed org for one command",
		output:  "Render organizations and the active selection as table or json",
		quiet:   fmt.Sprintf("Suppress %s stderr notes; the organization list remains on stdout", command),
	}
}

func withOutput(help commandGlobalFlagHelp, output, quiet string) commandGlobalFlagHelp {
	help.output = output
	help.quiet = quiet
	return help
}

func withQuiet(help commandGlobalFlagHelp, quiet string) commandGlobalFlagHelp {
	help.quiet = quiet
	return help
}

func withNoColor(help commandGlobalFlagHelp, noColor string) commandGlobalFlagHelp {
	help.noColor = noColor
	return help
}

var globalFlagHelpByCommand = buildGlobalFlagHelp()
