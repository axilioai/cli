# axilio

The Axilio command-line interface: acquire and drive real phones from your
terminal, and inspect sessions, runs, and API keys. A single static Go binary,
built to be driven by people and coding agents alike.

```bash
axilio sessions start                 # start a phone session
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
- **A scripting surface**: successful runnable application commands emit one
  JSON document under `-o json`; `sessions start --export` is a separate exact
  shell contract and rejects JSON. Commands support quiet non-interactive
  operation and return stable exit codes.

## Install

### Homebrew (macOS, recommended)

```bash
brew install axilioai/tap/axilio
```

Homebrew installs the executable and both offline manual formats. Read the
terminal page with `man axilio`, or run `axilio help --html` to print a
clickable `file://` URL for the browser edition.

### curl (macOS / Linux)

```bash
curl -fsSL https://axilio.ai/install.sh | sh

# Optional: choose both destinations explicitly.
curl -fsSL https://axilio.ai/install.sh |
  INSTALL_DIR="$HOME/.local/bin" MAN_DIR="$HOME/.local/share/man/man1" sh
```

The script installs the latest release for your OS/arch. Set `VERSION=v0.7.0`
to pin a release. It installs both manuals to `MAN_DIR` when set; otherwise it
derives `<prefix>/share/man/man1` only when `INSTALL_DIR` ends in `/bin` or
`/sbin`. An arbitrary executable layout or an unavailable manual destination
produces a warning but does not undo a successful binary install. The script is
[auditable on GitHub](https://github.com/axilioai/cli/blob/main/install.sh).

### go install

```bash
go install github.com/axilioai/cli/cmd/axilio@latest
```

This puts only the `axilio` binary in `$(go env GOPATH)/bin` — make sure that's
on your `PATH`. `go install` does not register the manual page.

For Homebrew, run `brew upgrade axilio` (`axilio upgrade` prints that guidance,
while `axilio upgrade --check` still checks GitHub for a newer release).
For curl installs, rerun the curl installer to update both the executable and
manual; the standalone `axilio upgrade` replaces only the executable. For
`go install`, rerun the `go install` command.

## Offline manual

```bash
man axilio
```

Homebrew and compatible curl-prefix installs make `axilio(1)` discoverable by
`man` and make the browser edition discoverable through `axilio help --html`.
Every GitHub release archive also contains the source page at
`man/axilio.1` and a self-contained browser edition at `man/axilio.1.html`:

```bash
open man/axilio.1.html       # macOS
xdg-open man/axilio.1.html   # Linux
```

The HTML page is generated from the same versioned command documentation as
the roff page and needs no remote stylesheet or JavaScript. Windows archives
carry both files for reference, but Windows and `go install` do not integrate
the roff file with a system manual-page viewer. If you choose a custom
`MAN_DIR`, ensure that directory is on your `MANPATH`.

## Contributing

Install the repository-owned Git hooks once after cloning, then run the doctor
whenever the remote or hook tooling changes:

```bash
scripts/install-git-hooks.sh
scripts/install-git-hooks.sh --check
```

The pre-push gate validates the configured Git email and the complete history
of every outgoing branch or tag against the reviewed identity policy on
`origin/main`. Hosted CI performs the same validation and remains authoritative.
The installer refuses to replace another active `core.hooksPath`; chain the
versioned `.githooks/pre-push` hook manually in that case.

## Quick start

Two ways in, depending on who's driving.

### Give it to your agent (recommended)

```bash
axilio login   # you sign in once (browser) — the one step your agent can't do
axilio init    # writes a skill for your agent (auto-detects claude / codex / cursor)
```

Then tell your agent:

> Use the axilio CLI to take a phone, open the browser, search for something —
> then hand me the SDK script.

The agent explores and drives the phone live through the CLI, and the SDK
script it hands back is the durable thing you keep and run without it.

> **If you are a coding agent reading this:** run `axilio init`, then read the
> skill file it writes and follow it. If any `axilio` command exits with code 3,
> stop and ask your human to run `axilio login` — browser sign-in needs them.

Own dedicated phones? Point the session at one: `axilio phones mine` for the
`phone_id`, then `axilio sessions start --phone-id <id>`.

### Drive it yourself

```bash
axilio login                 # sign in (browser OAuth, or --api-key)
axilio doctor                # one-shot check: auth, connectivity, account, environment
axilio phones list           # phones you can claim
axilio sessions start        # acquire a phone; the session remains active until stopped
axilio phone observe         # see the screen
axilio sessions stop <id>    # release it
```

## Authentication

`axilio login` signs you in two ways.

**Browser (OAuth), the default.** Run `axilio login` on a terminal and it opens
your browser to authorize the CLI. The Axilio session token is stored in your OS
keychain (the file fallback requests mode `0600`, subject to umask; overwrites
preserve its existing mode) and refreshed automatically.

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
$XDG_CONFIG_HOME/axilio/config.json   (else ~/.config/axilio/config.json)
```

New config files request mode `0600`; umask may make that more restrictive.
Overwriting an existing file preserves its mode.

Precedence rules:

- Credentials resolve in this order: `--api-key`, `AXILIO_API_KEY`, the saved
  config API key, then the saved OAuth session.
- The organization resolves from `--org`, `AXILIO_ORG`, the saved active
  organization, then the OAuth session default. Note that each API key is scoped
  to the organization that created it and cannot switch organizations with
  `--org`.
- API host resolves from `--base-url`, `AXILIO_BASE_URL`, saved `base-url`,
  then `https://api.axilio.ai`.
- Phone command session selection precedence is `--session`, `AXILIO_SESSION`,
  the only locally saved session, the most recently started session, then an
  ambiguity error.

`axilio logout` clears the saved key, OAuth session, and active organization.

## Commands

| Command | What it does |
| --- | --- |
| `login` / `logout` / `status` | Store, remove, and check credentials. |
| `doctor` | One-shot setup check: auth, connectivity, account, environment. |
| `config` / `config set` / `config unset` | Show and edit CLI configuration (API host, paths, auth). |
| `orgs list` / `orgs use` / `orgs clear` | List and switch the active organization (OAuth sessions). |
| `upgrade` / `upgrade --check` | Update axilio to the latest release (Homebrew installs defer to `brew upgrade`). |
| `init` | Drop an agent skill into the repo so a coding agent can drive phones via the CLI and emit SDK code. Bare `init` auto-detects the agent(s) from repo markers and checks you're signed in; `--agent claude\|codex\|cursor` picks one. Re-run with `--force` after `axilio upgrade` to refresh. |
| `phones list` | List phones you can start a session on right now (shared pool + your free dedicated phones). |
| `phones mine` | List your org's dedicated phones, including ones currently in use (find a `phone_id` to pin). |
| `sessions start` / `stop` / `list` / `current` | Start, stop, and inspect phone sessions. |
| `phone observe` / `find` / `find-text` / `tap` / `long-press` / `swipe` / `type` / `key` / `screenshot` / `wait-for` | Observe and control the selected phone session. |
| `phone send` | Upload a local image/video and push it to the selected session's phone. |
| `workflows list` | Discover workflow IDs by recency or name search. |
| `workflows create` / `get` / `delete` | Create a workflow (optionally seeding its first code revision from a file), inspect its details and run statistics, or delete it. |
| `workflows pull` / `push` / `revisions` / `restore` | Round-trip workflow code: pull the current source, push a local file back as a new revision with an optional message, list revision history, and restore an earlier revision. |
| `runs list` / `runs start` / `runs get` / `runs cancel` | Start, inspect, and cancel workflow runs. |
| `api-keys list` / `create` / `delete` | Manage your organization's API keys. |
| `uploads add` / `uploads list` / `uploads push` / `uploads delete` | Store files, inspect quota, deliver uploads, and free library quota. |
| `completion <shell>` | Generate a shell-completion script. |

### Global flags

| Flag | Meaning |
| --- | --- |
| `-o, --output table\|json` | Result format for runnable application commands (default `table`). JSON success is exactly one document. Built-in help, completion, and version remain text; `sessions start --export` rejects JSON. |
| `-q, --quiet` | Preserve primary result data, warnings, and errors; suppress human acknowledgments, notes, progress, and prompts. Destructive commands still require `--yes`. |
| `--no-color` | Disable ANSI color in human-oriented output. |
| `--api-key` | API key for API-backed commands; overrides `AXILIO_API_KEY` and the saved key. |
| `--base-url` | API host for API-backed commands; overrides `AXILIO_BASE_URL` and saved `base-url`. |
| `--org` | OAuth organization slug or ID; overrides `AXILIO_ORG` and the saved active org. |

Run `axilio <command> --help` for the flags on any command. The root-only
`axilio --version` flag prints build information as text.

## Scripting and agents

The CLI's output is a contract, not just cosmetics.

- **Streams:** primary data and successful action acknowledgments use stdout.
  Notes, progress, prompts, warnings, and errors use stderr. Warnings and errors
  remain visible in every output mode.
- **`-o json`** writes exactly one structured result to stdout for every
  successful runnable application command — data verbs and `sessions stop`
  emit the API response, action verbs emit a small acknowledgment (e.g.
  `{"action":"tap","x":540,"y":1200}`), and deletions emit
  `{"id":...,"deleted":true}` — so those successes pipe cleanly into `jq`.
  Optional human acknowledgments, notes, progress, and prompts are suppressed.
  Warnings and errors remain on stderr. Built-in help and completion commands,
  bare parent-command help, `--help`, and `--version` remain text. `sessions
  start --export` emits only an eval-able `export` line and cannot be combined
  with JSON output.
- **`-q, --quiet`** preserves primary data on stdout and warnings/errors on
  stderr while suppressing human acknowledgments, notes, progress, and prompts.
  Destructive commands (`sessions stop`, `runs cancel`, `api-keys delete`,
  `uploads delete`) prompt only when stdin is a terminal. Quiet, JSON, and
  redirected execution require `--yes` to proceed.
- **Stable exit codes** let you branch on the outcome without parsing stderr:

  | Code | Meaning | Examples |
  | --- | --- | --- |
  | `0` | success | |
  | `1` | error | failure that does not fit a more specific category |
  | `2` | usage | invalid command syntax, argument count, or value; HTTP 400/422 |
  | `3` | auth | missing or invalid credentials, unauthorized access, or permission denied; HTTP 401/403 |
  | `4` | not found | requested resource, phone allocation, or on-screen element not found; HTTP 404 |
  | `5` | timeout | operation exceeded its timeout or deadline; HTTP 408 |
  | `6` | unavailable | service or phone unavailable/offline, rate limit, or server failure; HTTP 429/5xx |
  | `7` | canceled | operation canceled by the user, shell, or system |

  For example, `axilio phone find "..."` returning `4` means no matching
  element was found. Failures without a more specific classification return
  `1`.

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

Each terminal or agent process can select its own session and drive its own
phone at once. Pin a phone to a shell with `AXILIO_SESSION`:

```bash
# in terminal A
eval "$(axilio sessions start --export)"      # sets AXILIO_SESSION for this shell
axilio phone observe                          # drives A's phone

# in terminal B (a second phone, concurrently)
eval "$(axilio sessions start --export)"
axilio phone observe                          # drives B's phone
```

Sessions remain active in Axilio until stopped. The CLI saves connection
information locally so later phone commands can reconnect. `axilio sessions
list` shows sessions saved locally (a `*` marks the one selected in the current
shell); `--remote` lists all active Axilio sessions. Phone command session
selection precedence is `--session`, `AXILIO_SESSION`, the only locally saved
session, the most recently started session, then an ambiguity error.

### Start and inspect runs

```bash
axilio workflows list --search checkout
axilio runs start <workflow-id>
axilio runs start <workflow-id> --count 3
axilio runs list
axilio runs get <run-id>
axilio runs cancel <run-id> --yes
```

`runs start --count` accepts values from 1 through 1000, inclusive, and
`--start-timeout` accepts 60 through 86400 seconds (0 omits the field and uses
the server default). The list commands bound their pagination the same way:
`runs list`/`workflows list --limit` accept 1-500, `workflows revisions
--limit` accepts 1-200, and `uploads list --limit` accepts 1-100 with a
non-negative `--offset`. All ranges are rejected with a usage error before
authentication or an API request.

### Author and version workflow code

```bash
axilio workflows create checkout-flow --platform android --code checkout.py
axilio workflows pull <workflow-id> --out checkout.py
axilio workflows push <workflow-id> checkout.py -m "handle 2FA"
axilio workflows revisions <workflow-id>
axilio workflows restore <workflow-id> <revision-id>
axilio workflows get <workflow-id>
axilio workflows delete <workflow-id> --yes
```

`pull` prints the current revision's source to stdout (pipe or redirect it), or
writes it to `--out`. `push` saves a local file as a new revision; the server
deduplicates by content hash, so pushing unchanged source is a reported no-op.
`restore` copies an earlier revision's source into a new revision, keeping the
action visible in history. Source files are capped at 256 KiB, preflighted
before any request.

### Manage API keys

```bash
axilio api-keys list
axilio api-keys create ci-key                 # the secret is shown once
axilio api-keys delete <key-id> --yes
```

### Send and reuse uploads

```bash
axilio phone send ./photo.jpg --wait          # add + push to selected session

axilio uploads add ./clip.mp4                  # store once
axilio uploads list                            # discover id and quota
axilio uploads push <upload-id> --phone-id <phone-id> --wait
axilio uploads delete <upload-id> --yes        # recalls copies held/received on phones
```

## Shell completions

```bash
axilio completion zsh   > "${fpath[1]}/_axilio"     # zsh
axilio completion bash  > /etc/bash_completion.d/axilio
axilio completion fish  > ~/.config/fish/completions/axilio.fish
axilio completion powershell | Out-String | Invoke-Expression

# Omit command and flag descriptions when a shell needs a smaller script:
axilio completion zsh --no-descriptions > "${fpath[1]}/_axilio"
```

Run `axilio completion --help` or
`axilio completion <bash|zsh|fish|powershell> --help` for per-shell instructions.

## Help and support

- `axilio --help` and `axilio <command> --help` for usage.
- `man axilio` for the comprehensive offline manual when installed through
  Homebrew or a compatible curl prefix.
- `axilio help --html` for a clickable URL to the installed HTML manual.
- Docs: [https://docs.axilio.ai](https://docs.axilio.ai)
- Issues: [https://github.com/axilioai/cli/issues](https://github.com/axilioai/cli/issues)
