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
axilio doctor                # one-shot setup check: auth, connectivity, account, environment
axilio phones list           # phones you can claim
axilio sessions start        # acquire a phone (the lease persists)
axilio sessions list
axilio sessions stop <id>    # release it
```

Add `-o json` to any command for scripting; `--help` on any command; shell
completions via `axilio completion <shell>`.

## Scripting & agents

The CLI is built to be driven by scripts and coding agents, not just people:

- **`-o json`** — deterministic JSON on stdout for every command; human chrome
  (notes, prompts, spinners) stays on stderr and is suppressed in JSON mode.
- **`-q` / `--quiet`** — suppress the stderr chrome for non-interactive use.
  Destructive commands (`sessions stop`, `runs cancel`, `api-keys delete`) never
  prompt in `--quiet` or JSON mode; pass `--yes` to proceed.
- **Stable exit codes** — branch on the exit code instead of parsing stderr:

  | Code | Meaning | Examples |
  |------|---------|----------|
  | `0` | success | |
  | `1` | error | unclassified failure, executor-internal error |
  | `2` | usage | bad flag/arg, unknown command, invalid input |
  | `3` | auth | no API key, unauthorized (HTTP 401/403) |
  | `4` | not found | element/session/resource not found (HTTP 404) |
  | `5` | timeout | a call or wait loop deadline (retryable) |
  | `6` | unavailable | network / device offline / server error (transient) |
  | `7` | canceled | the operation was canceled |

  The codes map the phone driver's error taxonomy and the API's HTTP status onto
  one stable table, so `axilio phone find "..."` returning `4` means "no match"
  regardless of transport.

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
