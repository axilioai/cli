# axilio

The Axilio command-line interface: acquire and drive real phones from your
terminal, and inspect sessions, runs, and API keys. A single static Go binary,
built to be driven by people and coding agents alike.

```bash
axilio sessions start                 # start a session on a shared-pool phone, billed per second
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

### PowerShell (Windows)

```powershell
irm https://axilio.ai/install.ps1 | iex

# Optional: pin a release or choose the destination.
$env:VERSION = "v0.12.0"; $env:INSTALL_DIR = "$env:USERPROFILE\bin"; irm https://axilio.ai/install.ps1 | iex
```

The script works in Windows PowerShell 5.1 and PowerShell 7, and needs no
administrator rights. It installs the latest release for your architecture
(amd64 or arm64) to `%LOCALAPPDATA%\Programs\axilio\bin`, verifies the archive
against the release `checksums.txt`, and adds the directory to your user `PATH`
when it is not already there. Open a new terminal afterwards. The HTML manual
goes to `man\axilio.1.html` next to `axilio.exe`, where `axilio help --html`
finds it. The script is
[auditable on GitHub](https://github.com/axilioai/cli/blob/main/install.ps1).

### go install

```bash
go install github.com/axilioai/cli/cmd/axilio@latest
```

This puts only the `axilio` binary in `$(go env GOPATH)/bin` — make sure that's
on your `PATH`. `go install` does not register the manual page.

For Homebrew, run `brew upgrade axilio` (`axilio upgrade` prints that guidance,
while `axilio upgrade --check` still checks GitHub for a newer release).
For curl installs, rerun the curl installer to update both the executable and
manual; the standalone `axilio upgrade` replaces only the executable. The same
holds for the PowerShell installer on Windows. For `go install`, rerun the
`go install` command.

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
start man\axilio.1.html      # Windows
```

The HTML page is generated from the same versioned command documentation as
the roff page and needs no remote stylesheet or JavaScript. Windows has no
system manual-page viewer, so on Windows use the HTML edition: the PowerShell
installer puts it where `axilio help --html` finds it. `go install` installs
neither manual. If you choose a custom
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

Use an agent with terminal access to the environment where Axilio will run.
A cloud agent runs commands in its own environment; installing the CLI on your
computer does not give a separate cloud chat access to it.

```bash
axilio doctor  # check existing authentication and connectivity
```

If credentials are missing or rejected, sign in yourself in this environment:

```bash
axilio login
```

For forbidden access (HTTP 403), check your organization or ask its administrator
for access instead of signing in again. Once access is ready:

```bash
axilio init    # writes a skill for your agent (auto-detects claude / codex / cursor)
axilio doctor  # verify readiness after setup
```

In a fresh non-interactive folder, choose the known agent explicitly with
`axilio init --agent claude`, `axilio init --agent codex`, or
`axilio init --agent cursor`. Existing repository instructions are preserved.
`init` can write the skill successfully while warning that sign-in is missing;
use `doctor` to verify readiness. For a headless environment, securely inject
`AXILIO_API_KEY` there instead of using browser sign-in.

Then tell your agent:

> Help me complete one useful task on one of my organization's dedicated phones.
> Ask which app and result I want if I haven't said, use the axilio CLI to prove
> the flow, then hand me a standalone SDK script.

The agent guides you through app preparation, completes the approved task, and
hands back a script you can run again. It honors your Python/Go preference or
existing Python/Go project; otherwise it uses Python.

> **If you are a coding agent reading this:** run `axilio init`, then read the
> skill file it writes and follow it. On exit code 3, inspect the error detail.
> Missing or rejected credentials need human sign-in through `axilio login` in
> the same environment, or secure `AXILIO_API_KEY` injection in a headless setup.
> Forbidden access (HTTP 403) needs the correct organization or administrator
> assistance. Verify with `axilio doctor` and resume the task. Browser and app
> sign-in need the human.

Without `--phone-id`, `sessions start` takes a phone from the shared pool, which
is billed per second of session time at your plan's rate. To use a dedicated
phone you rent instead (no phone-time charge), get its `phone_id` from
`axilio phones mine`, then run `axilio sessions start --phone-id <id>`.

### Drive it yourself

```bash
axilio login                 # sign in (browser OAuth, or --api-key)
axilio doctor                # one-shot check: auth, connectivity, account, environment
axilio phones list           # phones you can claim
axilio sessions start        # acquire a shared-pool phone, billed per second until stopped
axilio phone observe         # see the screen
axilio sessions stop <id>    # release it
```

## Authentication

`axilio login` signs you in two ways.

**Browser (OAuth), the default.** Run `axilio login` on a terminal and it opens
your browser to authorize the CLI. The Axilio session token is stored in your OS
keychain (macOS Keychain, Windows Credential Manager, or the Linux Secret
Service; the file fallback requests mode `0600`, subject to umask; overwrites
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

In PowerShell, pipe the variable the same way: `$env:AXILIO_API_KEY | axilio login`.

The API key is written to a language-agnostic config file that every Axilio SDK
also reads, so one login makes the CLI and the SDKs work:

```
$XDG_CONFIG_HOME/axilio/config.json   (else ~/.config/axilio/config.json)
%USERPROFILE%\.config\axilio\config.json   (Windows, when XDG_CONFIG_HOME is unset)
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
| `sessions recording <id>` | Look up a session recording's status and URL, or download the MP4 with `--out` (`--wait` polls while it is still processing). |
| `phone observe` / `find` / `find-text` / `tap` / `long-press` / `swipe` / `type` / `key` / `screenshot` / `wait-for` | Observe and control the selected phone session. |
| `phone tree` | Print the phone's accessibility tree (roles, names, resource ids, node ids) for a session with accessibility mode on. |
| `phone accessibility status` / `enable` / `disable` | Show whether the session's accessibility tree is on, or turn it on or off mid-session. |
| `phone send` | Upload a local image/video and push it to the selected session's phone. |
| `agents list` | Discover agent IDs by name search or kind. |
| `agents create` / `get` / `delete` | Create an agent (optionally seeding its first revision from a code file with `--code`), inspect its details, or delete it. |
| `agents pull` / `push` / `revisions` / `rollback` | Round-trip agent code: pull the current code, push a local file back as a new revision with an optional message, list revision history, and bring an earlier revision back as the newest. |
| `runs start` / `runs watch` | Start a run of an agent, optionally following it to the end with `--watch`, or attach to a running run (or replay a finished one) by ID. |
| `runs list` / `runs get` / `runs cancel` | List runs (filter by agent, schedule, mode, or status), inspect one, or cancel queued and running work. |
| `runs metrics` | Per-agent run counts, success rate, and cost over a window. |
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
  start --export` emits only a shell assignment (`export` by default, or the
  `--shell powershell|cmd` form) and cannot be combined with JSON output.
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
  | `6` | unavailable | service or phone unavailable/offline, the requested phone lacks accessibility mode, rate limit, or server failure; HTTP 429/5xx |
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
axilio sessions start --phone-id ph_123      # your phone's ID, from `axilio phones mine`

axilio phone observe -o json                  # text + elements with coordinates
axilio phone find "the search box" -o json    # locate a target semantically
axilio phone tap --query "the search box"     # act on it
axilio phone type "androiddev"
axilio phone key enter
axilio phone wait-for "Results" --timeout 15s
axilio phone screenshot --out screen.png

axilio sessions stop <id>
```

### Accessibility mode

Accessibility mode is off by default. Start a session with `--accessibility`
to get the phone's accessibility tree. That requires a phone that supports it:
only such phones are claimed, and a named `--phone-id` that cannot provide it
exits `6`. `agents create --accessibility` turns it on for an agent's
runs. While it is on, the accessibility service is visible to apps on the
phone. With the tree on, the locator verbs take `--role`, `--name`, `--id` (and
`--exact`), and `--strategy` picks the resolver (`auto`, `vision`, or
`accessibility`):

```bash
axilio sessions start --accessibility                # off unless you ask for it

axilio phone tree                                    # role "name" #resource-id @x,y [node-id]
axilio phone tap --role button --name "Log in"
axilio phone tap --id com.example.app:id/login
axilio phone find --role textbox --name Email -o json
axilio phone wait-for --role progressbar --gone
axilio phone tap --query "the log in button" --strategy vision   # skip the tree

axilio phone accessibility status
axilio phone accessibility disable                   # where status says Toggleable
```

On a session without the tree, the tree-only flags exit `2`.

### Parallel sessions

Each terminal or agent process can select its own session and drive its own
phone at once. Pin a phone to a shell with `AXILIO_SESSION`. Replace `ph_123`
and `ph_456` with your phone IDs from `axilio phones mine`:

```bash
# in terminal A
eval "$(axilio sessions start --phone-id ph_123 --export)"   # sets AXILIO_SESSION for this shell
axilio phone observe                                         # drives A's phone

# in terminal B (a second phone, concurrently)
eval "$(axilio sessions start --phone-id ph_456 --export)"
axilio phone observe                                         # drives B's phone
```

In PowerShell, pipe the assignment to `Invoke-Expression`. On Windows,
`--export` prints PowerShell syntax by default; Git Bash and MSYS2 (which set
`SHELL`) get the POSIX form. `--shell posix|powershell|cmd` overrides the
choice.

```powershell
axilio sessions start --phone-id ph_123 --export | Invoke-Expression   # sets $env:AXILIO_SESSION
axilio phone observe
```

Sessions remain active in Axilio until stopped. The CLI saves connection
information locally so later phone commands can reconnect. `axilio sessions
list` shows sessions saved locally (a `*` marks the one selected in the current
shell); `--remote` lists all active Axilio sessions. Phone command session
selection precedence is `--session`, `AXILIO_SESSION`, the only locally saved
session, the most recently started session, then an ambiguity error.

### Start and inspect runs

```bash
axilio agents list --search checkout
axilio runs start --agent <agent-id>
axilio runs start --agent <agent-id> --watch
axilio runs watch <run-id>
axilio runs list --agent <agent-id> --status failed
axilio runs get <run-id>
axilio runs metrics --agent <agent-id>
axilio runs cancel <run-id> --yes
```

`runs start` runs the agent's current revision: a code agent runs its Python,
an instructions agent hands its task to a model. `--watch` follows the run
exactly like `runs watch`. `--start-timeout` accepts 60 through 86400 seconds
(0 omits the field and uses the server default). The list commands bound their
pagination the same way: `runs list`/`agents list --limit` accept 1-500,
`agents revisions --limit` accepts 1-200, and `uploads list --limit` accepts
1-100 with a non-negative `--offset`. All ranges are rejected with a usage
error before authentication or an API request.

`runs metrics` covers the last 7 days unless you pass `--from` and `--to`. The
success rate is completed runs over completed and failed runs.

### Author and version agent code

```bash
axilio agents create checkout-flow --code checkout.py
axilio agents pull <agent-id> --out checkout.py
axilio agents push <agent-id> checkout.py -m "handle 2FA"
axilio agents revisions <agent-id>
axilio agents rollback <agent-id> <revision-id>
axilio agents get <agent-id>
axilio agents delete <agent-id> --yes
```

`pull` prints the current revision's code to stdout (pipe or redirect it), or
writes it to `--out`. `push` saves a local file as a new revision, which
becomes the one runs use. `rollback` copies an earlier revision forward as the
newest one, keeping the action visible in history. Code files are capped at
256 KiB, preflighted before any request.

### Upgrading from 0.13

0.14.0 follows the Agents release of the API and has three breaking changes:

- `workflows` is now `agents`, and `workflows restore` is now `agents
  rollback`. `runs start` takes `--agent <agent-id>` instead of a positional
  workflow ID, and `--count` is gone. `sessions start --workflow` is gone and
  `sessions list --history --workflow` is now `--agent`.
- `runs history` and `runs stats` are removed. `runs list` now filters by
  `--agent`, `--schedule`, `--mode`, and `--status`, and `runs metrics`
  replaces `runs stats`.
- The run's `error_message` field is now `error` in JSON output, including the
  final `watch_end` line of `runs watch`.

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
axilio uploads delete <upload-id> --yes        # library only; copies on phones stay
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
