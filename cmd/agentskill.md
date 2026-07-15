# Axilio: drive a real phone from the CLI, then hand back SDK code

Axilio gives you real mobile phones on demand. As the agent, you **explore and drive
a phone live through the `axilio` CLI**, then write the equivalent **Python SDK
script** as the durable deliverable the user keeps and runs later without you.

## Setup

- The `axilio` CLI must be installed and signed in — `axilio doctor` should be
  all-green. Install with `brew install axilioai/tap/axilio` or
  `curl -fsSL https://axilio.ai/install.sh | sh`; sign in with `axilio login`
  (browser) or by setting `AXILIO_API_KEY`.
- For the deliverable, the Python SDK: `pip install axilio`.

## Loop: explore and drive via the CLI

Pass `-o json` for machine-readable output. Work one step at a time and `observe`
after actions to confirm the screen changed. Prefer semantic `find` / `find-text`
over raw coordinates.

```bash
axilio sessions start --phone-type android    # lease a phone (becomes the current session)

axilio phone observe -o json                  # text + UI elements + coordinates on screen
axilio phone find "the search box" -o json    # locate a target by natural-language query
axilio phone tap --query "the search box"     # tap it
axilio phone type "androiddev"                # type into the focused field
axilio phone key enter                        # press a key: enter, back, home, ...
axilio phone find-text "Results" -o json      # locate visible text
axilio phone wait-for "Results" --timeout 15s # wait for text to appear
axilio phone screenshot --out screen.png      # capture the screen

axilio sessions stop <session-id>             # release the phone
```

Full verb list and flags: `axilio phone --help`. To drive several phones at once,
`eval "$(axilio sessions start --export)"` pins a phone to the current shell via
`AXILIO_SESSION`, so each terminal drives its own.

## Deliverable: write the SDK script

Once you've worked the task out live, write a standalone Python script with the SDK.
The CLI verbs map 1:1 onto the driver, so translate what worked into code:

```python
from axilio.platform import Client

client = Client()  # reads AXILIO_API_KEY from the environment

# Acquire a phone, drive it, and release it automatically on exit.
with client.session("android") as driver:
    driver.find(query="the search box").tap()
    driver.type_text("androiddev")
    driver.key_press("enter")
    driver.wait_for_text("Results", timeout=15)
    driver.screenshot()  # -> bytes (PNG)
```

`client.session(...)` allocates a phone, opens the control channel, yields a
`MobileDriver`, and releases the phone when the `with` block exits.

### Driver reference

| Method | Returns | Notes |
|--------|---------|-------|
| `driver.observe(ocr_engine=None)` | `Screen` | text + elements + coordinates |
| `driver.find(query, timeout=10.0)` | `Element` | semantic locate; raises if not found |
| `driver.find_text(text, exact=False)` | `Element \| None` | locate visible text |
| `driver.wait_for_text(text, timeout=10.0)` | `Element` | poll until the text appears |
| `driver.type_text(text)` | `None` | type into the focused field |
| `driver.key_press(key)` | `None` | `"enter"`, `"back"`, `"home"`, … |
| `driver.tap(coords)` / `driver.long_press(coords)` / `driver.swipe(start, end)` | `None` | coordinate input |
| `driver.screenshot()` | `bytes` | PNG |

`find(...)` and `find_text(...)` return an `Element` with `.tap()` and `.long_press()`.

## Rules

- Explore with the CLI; the **SDK script is the deliverable** — the user runs it
  later with no agent in the loop.
- One action per step, then `observe` to verify. Prefer `find` / `find-text` over
  raw coordinates.
- Always release the phone: `axilio sessions stop <id>`, or let the SDK `with`
  block release it.
