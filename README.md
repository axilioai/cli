# axilio

The Axilio command-line interface: acquire and drive real phones from your
terminal, and inspect sessions, runs, and API keys. A single static Go binary,
built to be driven by people and coding agents alike.

```bash
axilio sessions start                 # lease a phone
axilio phone observe                  # see what's on screen
axilio phone tap --query "the search box"
axilio phone type "androiddev"
axilio sessions stop <id>             # release it
```

## What it is

Axilio gives you a fleet of real mobile phones on demand. The CLI is a thin,
standalone client over the generated [`platform-go`](https://github.com/axilioai/platform-go)
SDK, decoupled from any one SDK language so it serves every user as
multi-language SDK support lands. It does three things:

- **Lifecycle and inspection**: sign in, list phones, start/stop sessions, view
  runs, manage API keys.
- **Phone control**: observe the screen and drive it (find, tap, type, swipe,
  key) over the phone's control channel, using the same vision primitives as the
  SDK.
- **A scripting surface**: deterministic `-o json` and stable exit codes on every
  command, so an agent or shell script drives it without parsing prose.

## Install

`go install` works today:

```bash
go install github.com/axilioai/cli@main
```

This installs the `axilio` binary into `$(go env GOPATH)/bin`. Make sure that's
on your `PATH`.

Homebrew, a `curl | sh` installer, and versioned `@latest` builds ship with the
first tagged release:

```bash
# coming with the first release
brew install axilioai/tap/axilio
curl -fsSL https://axilio.ai/install.sh | sh
go install github.com/axilioai/cli@latest
```

## Quick start

```bash
axilio login                 # sign in (browser OAuth, or --api-key)
axilio doctor                # one-shot check: auth, connectivity, account, environment
axilio phones list           # phones you can claim
axilio sessions start        # acquire a phone; the lease persists until you stop it
axilio phone observe         # see the screen
axilio sessions stop <id>    # release it
```

## Authentication

`axilio login` signs you in two ways.

**Browser (OAuth), the default.** Run `axilio login` on a terminal and it opens
your browser to authorize the CLI. The Axilio session token is stored in your OS
keychain (with a `0600` file fallback) and refreshed automatically.

```bash
axilio login                                   # opens the browser (OAuth)
```

**API key.** Pass a key, or pipe one in, to store an `axl_` key instead, which
the SDKs also read:

```bash
axilio login --api-key axl_xxx                 # store a key directly
echo "$AXILIO_API_KEY" | axilio login          # non-interactive (pipe the key in)
```

The API key is written to a language-agnostic config file that every Axilio SDK
also reads, so one login makes the CLI and the SDKs work:

```
$XDG_CONFIG_HOME/axilio/config.json   (else ~/.config/axilio/config.json), mode 0600
```

Credentials resolve in this order (first wins): an explicit API key (`--api-key`
flag, `AXILIO_API_KEY` env, or config file), then your OAuth session. The API
host resolves via `--base-url` / `AXILIO_BASE_URL` / config, defaulting to
`https://api.axilio.ai`. `axilio logout` clears both the key and the OAuth
session.

## Commands

| Command | What it does |
| --- | --- |
| `login` / `logout` / `status` | Store, remove, and check credentials. |
| `doctor` | One-shot setup check: auth, connectivity, account, environment. |
| `config` / `config set` / `config unset` | Show and edit CLI configuration (API host, paths, auth). |
| `org list` / `org use` / `org clear` | List and switch the active organization (OAuth sessions). |
| `upgrade` / `upgrade --check` | Update axilio to the latest release (Homebrew installs defer to `brew upgrade`). |
| `phones list` | List phones you can claim from the shared pool. |
| `sessions start` / `stop` / `list` / `current` | Acquire, release, and inspect phone leases. |
| `phone observe` / `find` / `find-text` / `tap` / `long-press` / `swipe` / `type` / `key` / `screenshot` / `wait-for` | Drive the current phone session. |
| `runs list` / `get` / `cancel` | Inspect and manage workflow runs. |
| `api-keys list` / `create` / `delete` | Manage your organization's API keys. |
| `completion <shell>` | Generate a shell-completion script. |

### Global flags

| Flag | Meaning |
| --- | --- |
| `-o, --output table\|json` | Output format (default `table`). |
| `-q, --quiet` | Suppress stderr chrome (notes and prompts) for non-interactive use. |
| `--no-color` | Disable colored output. |
| `--api-key` | Override the API key for this call. |
| `--base-url` | Override the API host for this call. |
| `--org` | Organization slug or id to act as for this call (OAuth sessions; also `AXILIO_ORG` / `org use`). |
| `-v, --version` | Print the version. |

Run `axilio <command> --help` for the flags on any command.

## Scripting and agents

The CLI's output is a contract, not just cosmetics.

- **`-o json`** gives every command a stable JSON shape on stdout. Human chrome
  (notes, prompts, spinners) goes to stderr and is suppressed in JSON mode, so a
  pipe into `jq` stays clean.
- **`-q, --quiet`** suppresses the stderr chrome entirely. Destructive commands
  (`sessions stop`, `runs cancel`, `api-keys delete`) never prompt in `--quiet`
  or JSON mode; pass `--yes` to proceed non-interactively.
- **Stable exit codes** let you branch on the outcome without parsing stderr:

  | Code | Meaning | Examples |
  | --- | --- | --- |
  | `0` | success | |
  | `1` | error | unclassified failure, executor-internal error |
  | `2` | usage | bad flag/arg, unknown command, invalid input |
  | `3` | auth | no API key, unauthorized (HTTP 401/403) |
  | `4` | not found | element/session/resource not found (HTTP 404) |
  | `5` | timeout | a call or wait loop deadline (retryable) |
  | `6` | unavailable | network / device offline / server error (transient) |
  | `7` | canceled | the operation was canceled |

  The codes map the phone driver's error taxonomy and the API's HTTP status onto
  one table, so `axilio phone find "..."` returning `4` means "no match"
  regardless of transport.

## Examples

### Drive a phone

Phone verbs target the current session (see [Parallel sessions](#parallel-sessions)).
The verbs are a 1:1 projection of the SDK's driver, so a session you explore here
maps directly onto SDK code.

```bash
axilio sessions start --phone-type android

axilio phone observe -o json                  # text + elements with coordinates
axilio phone find "the search box" -o json    # locate a target semantically
axilio phone tap --query "the search box"     # act on it
axilio phone type "androiddev"
axilio phone key enter
axilio phone wait-for "Results" --timeout 15s
axilio phone screenshot --out screen.png

axilio sessions stop <id>
```

### Parallel sessions

Each terminal or agent process can hold its own lease and drive its own phone at
once, with no shared state. Pin a phone to a shell with `AXILIO_SESSION`:

```bash
# in terminal A
eval "$(axilio sessions start --export)"      # sets AXILIO_SESSION for this shell
axilio phone observe                          # drives A's phone

# in terminal B (a second phone, concurrently)
eval "$(axilio sessions start --export)"
axilio phone observe                          # drives B's phone
```

`axilio sessions list` shows the leases this CLI holds (a `*` marks the one the
phone verbs target in the current shell); `--remote` lists all active sessions on
the server. Selection precedence: `--session <id>` flag, then `AXILIO_SESSION`,
then the sole active lease, then the most-recently-started one.

### Inspect runs and keys

```bash
axilio runs list
axilio runs get <run-id>
axilio runs cancel <run-id> --yes

axilio api-keys list
axilio api-keys create ci-key                 # the secret is shown once
axilio api-keys delete <key-id> --yes
```

## Shell completions

```bash
axilio completion zsh   > "${fpath[1]}/_axilio"     # zsh
axilio completion bash  > /etc/bash_completion.d/axilio
axilio completion fish  > ~/.config/fish/completions/axilio.fish
```

Run `axilio completion --help` for per-shell instructions.

## Help and support

- `axilio --help` and `axilio <command> --help` for usage.
- Docs: [https://docs.axilio.ai](https://docs.axilio.ai)
- Issues: [https://github.com/axilioai/cli/issues](https://github.com/axilioai/cli/issues)
