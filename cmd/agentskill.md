# Axilio: drive a real phone from the CLI, then hand back SDK code

Axilio gives you real mobile phones on demand. As the agent, you **explore and drive
a phone live through the `axilio` CLI**, then write the equivalent **SDK script** as
the durable deliverable the user keeps and runs later without you.

## Setup

- The `axilio` CLI must be installed and signed in — `axilio doctor` should be
  all-green. Install with `brew install axilioai/tap/axilio` or
  `curl -fsSL https://axilio.ai/install.sh | sh`; sign in with `axilio login`
  (browser) or by setting `AXILIO_API_KEY`.

## Loop: explore and drive via the CLI

Pass `-o json` for machine-readable output. Work one step at a time and `observe`
after actions to confirm the screen changed.

```bash
axilio sessions start --phone-type android    # lease a phone (becomes the current session)

axilio phone observe -o json                  # text + UI elements + coordinates on screen
axilio phone find "the search box" -o json    # locate a target by natural-language query
axilio phone tap --query "the search box"     # tap it — describe it, don't measure it
axilio phone type "androiddev"                # type into the focused field
axilio phone key enter                        # press a key: enter, back, home, ...
axilio phone find-text "Results" -o json      # locate visible text
axilio phone wait-for "Results" --timeout 15s # wait for text to appear
axilio phone screenshot --out screen.png      # look at the screen (fine — see the rule below)
axilio phone long-press --query "the first message"
axilio phone swipe --from-query "the photo" --to-query "the trash icon"
axilio phone swipe --raw 540 1500 540 500     # a scroll: no element to aim at

axilio sessions stop <session-id>             # release the phone
```

Full verb list and flags: `axilio phone --help`. To drive several phones at once,
`eval "$(axilio sessions start --export)"` pins a phone to the current shell via
`AXILIO_SESSION`, so each terminal drives its own.

## Rule: always use semantic selectors, never raw coordinates

**This is the most important rule here.** Find things by what they *are*, not by
where they happened to be on your screen:

```bash
axilio phone tap --query "the search box"   # ✅ do this
axilio phone tap --raw 540 1200             # ❌ not this
```

**Taking a screenshot is fine.** Look at the screen as much as you want. The rule is
about the *action*: don't measure a coordinate off the image and tap it. Look with
`screenshot` or `observe`, then act with `--query`.

Why this matters more than it looks:

- A coordinate is only true for one screen size, one layout, one scroll position, one
  font scale, one app version. The script you hand back runs later, unattended, on a
  *different phone from the pool* than the one you explored with. Hardcoded coordinates
  don't fail loudly when any of that shifts — they silently tap the wrong thing, and
  the user finds out from the consequences.
- `--query` routes through Axilio's grounding model, which is trained for exactly this
  and is markedly better at it than eyeballing pixels. Reading coordinates off a
  screenshot doesn't just risk breaking later — it's less accurate right now.

Every action verb takes a semantic target:

```bash
axilio phone tap --query "the search box"
axilio phone long-press --query "the first message"
axilio phone swipe --from-query "the photo" --to-query "the trash icon"
```

Coordinates require an explicit `--raw`, and the CLI will reject bare coordinates and
tell you this. The one place `--raw` is genuinely right is a target with no element to
aim at — a scroll gesture, a point on a map, a freehand drawing:

```bash
axilio phone swipe --raw 540 1500 540 500   # scrolling: nothing to describe
```

When you use `--raw`, leave a comment saying why the semantic path didn't fit. "It was
easier" is not a reason.

## Deliverable: ask which language, then write the script

Once you've worked the task out live, write a standalone script with the SDK.

**First, ask the user which SDK to write — don't assume:**

> Which SDK should I write this in — Python or Go?

Then follow the matching section below. What you explored maps onto the driver either
way; the language differs in how it installs, handles errors, and releases the phone.

<!-- lang:python -->
### Python

`pip install axilio`

`client.session(...)` is a context manager: it allocates a phone, opens the control
channel, yields a `MobileDriver`, and releases the phone when the `with` block exits —
including on exception. Always drive inside the `with` block.

```python
from axilio.platform import Client
from axilio.drivers import mobile

client = Client()  # reads AXILIO_API_KEY from the environment

with client.session("android") as driver:
    try:
        driver.find(query="the search box").tap()
        driver.type_text("androiddev")
        driver.key_press("enter")
        driver.wait_for_text("Results", timeout=15)
        screen = driver.screenshot()  # -> bytes (PNG)
    except mobile.ElementNotFoundError as e:
        # The screen wasn't what we expected — a dialog, a slow load, a changed app.
        print(f"could not find the target: {e}")
        raise
```

**Errors.** Every driver failure is an `AxilioError` subclass. Import the module
(`from axilio.drivers import mobile`) rather than the names directly — `mobile.TimeoutError`
and `mobile.ConnectionError` would otherwise shadow the builtins.

| Exception | When |
|---|---|
| `mobile.ElementNotFoundError` | `find` / `wait_for_text` found no match |
| `mobile.TimeoutError` | the call or wait deadline passed |
| `mobile.DeviceOfflineError` | the phone dropped off the control channel |
| `mobile.NotConnectedError` | driving after the session closed |
| `mobile.AxilioError` | base class — catch this to catch everything |

`find(...)` and `wait_for_text(...)` **raise** when there's no match. `find_text(...)`
returns `None` instead — check it before use.

| Method | Returns | Notes |
|--------|---------|-------|
| `driver.observe(ocr_engine=None)` | `Screen` | text + elements + coordinates |
| `driver.find(query, timeout=10.0)` | `Element` | semantic locate; **raises** if not found |
| `driver.find_text(text, exact=False)` | `Element \| None` | locate visible text |
| `driver.find_all_text(contains, pattern)` | `list[Element]` | every text match |
| `driver.wait_for_text(text, timeout=10.0)` | `Element` | poll until the text appears |
| `driver.wait_until_gone(text, timeout=10.0)` | `None` | poll until the text disappears |
| `driver.type_text(text)` | `None` | type into the focused field |
| `driver.key_press(key)` | `None` | `"enter"`, `"back"`, `"home"`, … |
| `driver.tap(coords)` / `driver.long_press(coords)` / `driver.swipe(start, end)` | `None` | coordinate input — see the semantic-selector rule |
| `driver.screenshot()` | `bytes` | PNG |

`find(...)` / `find_text(...)` return an `Element` that actions chain off:
`el.tap()`, `el.long_press()`, `el.type_into(text)`, `el.swipe_to(other)`. Prefer
`el.type_into("text")` over a separate `el.tap()` + `driver.type_text()`.
<!-- /lang:python -->

<!-- lang:go -->
### Go

`go get github.com/axilioai/platform-go`

Go has no context manager; `defer` is the equivalent. Allocate the phone through the
REST client, dial the returned `ControlURL`, and `defer` both the driver close and the
deallocate so the phone is released on every path out — including a panic.

```go
package main

import (
	"context"
	"log"
	"os"
	"time"

	platformgo "github.com/axilioai/platform-go"
	"github.com/axilioai/platform-go/client"
	"github.com/axilioai/platform-go/drivers/mobile"
	"github.com/axilioai/platform-go/option"
)

func main() {
	ctx := context.Background()
	cl := client.NewClient(option.WithAPIKey(os.Getenv("AXILIO_API_KEY")))

	a, err := cl.Phones.Allocate(ctx, &platformgo.PhoneAllocateRequest{
		PhoneType: platformgo.PhoneAllocateRequestPhoneTypeAndroid,
	})
	if err != nil {
		log.Fatalf("allocate: %v", err)
	}
	// Release the phone on every path out.
	defer func() {
		req := &platformgo.PhonesDeallocateRequest{}
		req.SetPhoneID(a.PhoneID)
		if _, derr := cl.Phones.Deallocate(context.Background(), req); derr != nil {
			log.Printf("deallocate: %v", derr)
		}
	}()

	if a.ControlURL == nil {
		log.Fatal("allocation returned no control URL")
	}
	d := mobile.ConnectRemote(*a.ControlURL)
	defer d.Close()

	el, err := d.Find("the search box")
	if err != nil {
		if mobile.IsElementNotFound(err) {
			log.Fatal("no search box on screen")
		}
		log.Fatalf("find: %v", err)
	}
	if err := el.Tap(); err != nil {
		log.Fatalf("tap: %v", err)
	}
	if err := d.TypeText("androiddev"); err != nil {
		log.Fatalf("type: %v", err)
	}
	if err := d.KeyPress(mobile.KeyEnter); err != nil {
		log.Fatalf("key: %v", err)
	}
	if _, err := d.WaitForText("Results", 15*time.Second, false); err != nil {
		log.Fatalf("wait: %v", err)
	}
	png, err := d.Screenshot()
	if err != nil {
		log.Fatalf("screenshot: %v", err)
	}
	_ = png
}
```

**Errors.** Every driver failure is a `*mobile.Error` carrying a `Code` and a
`Retryable` flag. Classify with the helpers rather than matching on strings:

| Helper | When |
|---|---|
| `mobile.IsElementNotFound(err)` | `Find` / `WaitForText` found no match |
| `mobile.IsTimeout(err)` | the call or wait deadline passed |
| `mobile.IsDeviceOffline(err)` | the phone dropped off the control channel |
| `mobile.IsRetryable(err)` | the failure is transient — retrying is reasonable |

For anything finer, `errors.As(err, &mobileErr)` and switch on `mobileErr.Code`.

| Method | Returns | Notes |
|--------|---------|-------|
| `driver.Observe(opts...)` | `(*Screen, error)` | text + elements + coordinates |
| `driver.Find(query, opts...)` | `(*Element, error)` | semantic locate; error if not found |
| `driver.FindText(text, exact, opts...)` | `(*Element, error)` | locate visible text |
| `driver.FindAllText(contains, pattern, opts...)` | `([]Element, error)` | every text match |
| `driver.WaitForText(text, timeout, exact, opts...)` | `(*Element, error)` | poll until the text appears |
| `driver.WaitUntilGone(text, timeout, exact, opts...)` | `error` | poll until the text disappears |
| `driver.TypeText(text)` | `error` | type into the focused field |
| `driver.KeyPress(key)` | `error` | `mobile.KeyEnter`, `"back"`, `"home"`, … |
| `driver.Tap(c)` / `driver.LongPress(c, ms)` / `driver.Swipe(start, end, ms)` | `error` | coordinate input — see the semantic-selector rule |
| `driver.Screenshot()` | `([]byte, error)` | PNG |

`Find(...)` / `FindText(...)` return an `*Element` that actions chain off:
`el.Tap()`, `el.LongPress(ms)`, `el.TypeInto(text)`, `el.SwipeTo(other, ms)`. Prefer
`el.TypeInto("text")` over a separate `el.Tap()` + `driver.TypeText()`.
<!-- /lang:go -->

## Rules

- **Semantic selectors, never raw coordinates.** See the rule above — it's the
  difference between a script that keeps working and one that silently misfires.
- Explore with the CLI; the **SDK script is the deliverable** — the user runs it
  later with no agent in the loop, so it must handle its own failures.
- Ask which language before writing. Don't assume Python.
- One action per step, then `observe` to verify.
- Always release the phone: `axilio sessions stop <id>` when exploring; in the
  script, the Python `with` block or the Go `defer` does it for you.
