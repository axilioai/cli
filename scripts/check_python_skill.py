#!/usr/bin/env python3
"""Check the Python half of the hosted agent skill against the real published SDK.

The skill is a prompt: an agent reads it and writes SDK code a customer runs
unattended. Every symbol it names has to exist, or we ship instructions for
writing code that raises AttributeError on the customer's machine.

The skill itself is no longer a file in this repo (AXI-1527): it's hosted at
the backend's /skill route, the same place `axilio init` and the dashboard's
"Get Agent Prompt" button fetch it from. This script checks the copy that is
actually live, which is stricter than checking a local file could ever be —
nothing here can drift from what a user actually receives.

The Go half is checked in-repo by cmd/agentskill_test.go, because the CLI depends
on platform-go. The Python half can't be: the CLI has no dependency on
platform-python, by design. So this runs in CI against a `pip install axilio`.

That gap is the whole point. platform-python releases on its own cadence, so the
skill rots on *its* schedule, not the CLI's — nothing in a CLI PR would notice.
This is why the workflow also runs on a timer.

Adding a language: add a <!-- lang:X --> block to the skill and a checker like
this one. See .github/workflows/skill-sync.yml.
"""

from __future__ import annotations

import os
import re
import sys
import urllib.request

SKILL_URL = os.environ.get("AXILIO_SKILL_URL", "https://api.axilio.ai/api/v1/skill")

# Floors guard against a vacuous pass: if a regex silently stops matching, the
# check would "succeed" while verifying nothing.
MIN_DRIVER_METHODS = 8
MIN_ELEMENT_METHODS = 4
MIN_EXCEPTIONS = 4


def fail(msg: str) -> None:
    print(f"FAIL: {msg}", file=sys.stderr)


def fetch_skill() -> str:
    # An identifying User-Agent: the API's WAF rejects the default
    # Python-urllib/* agent with a 403.
    req = urllib.request.Request(SKILL_URL, headers={"User-Agent": "axilio-skill-sync/1.0"})  # noqa: S310
    try:
        with urllib.request.urlopen(req, timeout=15) as resp:  # noqa: S310
            return resp.read().decode("utf-8")
    except Exception as e:
        print(f"FAIL: could not fetch the agent skill from {SKILL_URL}: {e}", file=sys.stderr)
        sys.exit(1)


def python_block(text: str) -> str:
    m = re.search(r"<!-- lang:python -->(.*?)<!-- /lang:python -->", text, re.S)
    if not m:
        print(f"FAIL: the hosted skill ({SKILL_URL}) has no <!-- lang:python --> block", file=sys.stderr)
        sys.exit(1)
    return m.group(1)


def documented(block: str, receiver: str) -> list[str]:
    """Distinct `receiver.method(` names, from both the tables and the fences."""
    found = re.findall(rf"\b{receiver}\.([a-z_][a-z0-9_]*)\(", block)
    return sorted(set(found))


def main() -> int:
    text = fetch_skill()
    block = python_block(text)
    errors = 0

    try:
        from axilio.drivers import mobile
        from axilio.drivers.mobile import MobileDriver
        from axilio.drivers.mobile.types import Element
        from axilio.platform import Client
    except ImportError as e:  # pragma: no cover
        print(f"FAIL: cannot import the axilio SDK: {e}", file=sys.stderr)
        return 1

    try:
        from importlib.metadata import version

        installed = version("axilio")
    except Exception:
        installed = "(unknown)"
    print(f"checking the hosted skill ({SKILL_URL}) against axilio {installed}")

    # 1. driver.<method>()
    methods = documented(block, "driver")
    if len(methods) < MIN_DRIVER_METHODS:
        fail(
            f"only found {len(methods)} documented driver methods ({methods}) — "
            "the parse is probably broken, which would make this check vacuous"
        )
        errors += 1
    for name in methods:
        if not hasattr(MobileDriver, name):
            fail(
                f"the hosted skill documents driver.{name}(), which does not exist on "
                "MobileDriver — the skill would teach an agent to write code that "
                "raises AttributeError"
            )
            errors += 1

    # 2. el.<method>() — the chained element actions
    el_methods = documented(block, "el")
    if len(el_methods) < MIN_ELEMENT_METHODS:
        fail(
            f"only found {len(el_methods)} documented el.* methods ({el_methods}) — "
            "the parse is probably broken"
        )
        errors += 1
    for name in el_methods:
        if not hasattr(Element, name):
            fail(f"the hosted skill documents el.{name}(), which does not exist on Element")
            errors += 1

    # 3. mobile.<Exception> — the error-handling guidance
    exceptions = sorted(set(re.findall(r"\bmobile\.([A-Z][A-Za-z0-9_]*Error)\b", block)))
    if len(exceptions) < MIN_EXCEPTIONS:
        fail(
            f"only found {len(exceptions)} documented exceptions ({exceptions}) — "
            "the error-handling guidance may have been dropped"
        )
        errors += 1
    for name in exceptions:
        if not hasattr(mobile, name):
            fail(
                f"the hosted skill documents mobile.{name}, which does not exist — an agent "
                "following the skill would write an except clause that itself raises"
            )
            errors += 1

    # 4. The entry point the whole Python section is built on.
    if not hasattr(Client, "session"):
        fail("Client.session() no longer exists; the Python section's `with` block is wrong")
        errors += 1

    # 5. Key names. This is a different axis from the checks above: key_press("home")
    # resolves fine as a *method* and fails at run time on the *argument* — the device's
    # named-key table has one entry. The skill documented "back"/"home" as usable and no
    # method-name check could have caught it, so the literals agents copy get checked
    # against the SDK's Key constants (which the SDK keeps in lockstep with the device).
    #
    # Only literals inside key_press(...) calls are checked — prose naming "home" as a
    # key that does NOT work is guidance, not an instruction to copy.
    from axilio.drivers.mobile.keys import Key

    valid_keys = {v for k, v in vars(Key).items() if not k.startswith("_") and isinstance(v, str)}
    used_keys = sorted(set(re.findall(r'key_press\(\s*"([^"]+)"\s*\)', block)))
    for name in used_keys:
        if name not in valid_keys:
            fail(
                f"the hosted skill shows key_press({name!r}), but the SDK's Key constants are "
                f"{sorted(valid_keys)} — the device's named-key table would reject it at run "
                "time. Don't document a key until it exists on the device side."
            )
            errors += 1

    if errors:
        print(f"\n{errors} problem(s). The skill and the published SDK have drifted.", file=sys.stderr)
        print("Fix the hosted skill to match the SDK, or the SDK to match the skill.", file=sys.stderr)
        return 1

    print(
        f"ok — {len(methods)} driver methods, {len(el_methods)} element actions, "
        f"{len(exceptions)} exceptions, {len(used_keys)} key name(s) all exist"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
