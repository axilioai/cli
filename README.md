# axilio CLI

The Axilio command-line interface: acquire and drive phones from your terminal,
and inspect sessions, runs, and phones. A single static Go binary.

## Install

```bash
# once published:
brew install axilioai/tap/axilio
# or grab a binary from the releases page
```

## Quick start

```bash
axilio login                 # stores your axl_ key in ~/.config/axilio/config.json (0600)
axilio status                # verify credentials + balance
axilio phones list           # phones you can claim
axilio sessions start        # acquire a phone (the lease persists)
axilio sessions list
axilio sessions stop <id>    # release it
```

Add `-o json` to any command for scripting; `--help` on any command; shell
completions via `axilio completion <shell>`.

## Design

- **Standalone Go CLI** (cobra + [fang](https://github.com/charmbracelet/fang) +
  [pterm](https://github.com/pterm/pterm)), decoupled from any one SDK language so
  it serves every user as multi-language SDK support lands.
- **Data on stdout, chrome on stderr.** Tables and JSON go to stdout; notes,
  prompts, and errors to stderr, and JSON mode suppresses the chrome so pipes into
  `jq` stay clean.
- **The CLI owns credentials.** `axilio login` writes a language-agnostic
  `~/.config/axilio/config.json` that every axilio SDK reads, so one login makes
  the CLI and the SDKs work.

## Status

Early scaffold. The API layer under `internal/api` is a temporary hand-written
client for the endpoints the CLI needs today; it will be replaced by the
Fern-generated Go SDK (`github.com/axilioai/platform-go`) generated from the same
OpenAPI spec.
