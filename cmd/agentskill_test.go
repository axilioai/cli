package cmd

import (
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/axilioai/platform-go/drivers/mobile"
)

// The skill is a prompt: an agent reads it and writes SDK code a customer runs
// unattended. So every symbol it names has to exist, or we are shipping
// instructions to write code that does not compile.
//
// The Go half is checkable here, because the CLI already depends on platform-go:
// parse the symbols out of the skill's <!-- lang:go --> block and look each one up
// on the real type. A rename in the SDK fails this test; a typo in the skill fails
// this test.
//
// The Python half cannot be checked from a Go test — the CLI has no dependency on
// platform-python, by design. That guard lives in .github/workflows/skill-sync.yml,
// which pip-installs axilio and introspects the <!-- lang:python --> block the same
// way. Adding a language means adding a block plus a checker; see that workflow.

// langBlock extracts one <!-- lang:X --> ... <!-- /lang:X --> section.
func langBlock(t *testing.T, lang string) string {
	t.Helper()
	re := regexp.MustCompile(`(?s)<!-- lang:` + lang + ` -->(.*?)<!-- /lang:` + lang + ` -->`)
	m := re.FindStringSubmatch(agentSkillBody)
	if m == nil {
		t.Fatalf("agentskill.md has no <!-- lang:%s --> block", lang)
	}
	return m[1]
}

// documentedMethods pulls the distinct `receiver.Method(` names out of a block,
// covering both the prose tables and the code fences.
func documentedMethods(block, receiver string) []string {
	re := regexp.MustCompile(`\b` + receiver + `\.([A-Z][A-Za-z0-9_]*)\(`)
	seen := map[string]bool{}
	var out []string
	for _, m := range re.FindAllStringSubmatch(block, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	return out
}

// TestSkillGoDriverMethodsExist checks every driver.X(...) the Go section names
// against the real MobileDriver.
func TestSkillGoDriverMethodsExist(t *testing.T) {
	block := langBlock(t, "go")
	methods := documentedMethods(block, "driver")
	if len(methods) < 8 {
		t.Fatalf("only found %d documented driver methods (%v) — the parse is probably broken, "+
			"which would make this test vacuous", len(methods), methods)
	}
	typ := reflect.TypeOf(&mobile.MobileDriver{})
	for _, name := range methods {
		if _, ok := typ.MethodByName(name); !ok {
			t.Errorf("agentskill.md documents driver.%s(), which does not exist on *mobile.MobileDriver — "+
				"the skill would teach an agent to write code that does not compile", name)
		}
	}
}

// TestSkillGoElementMethodsExist checks the chained element actions the Go section
// names (el.Tap, el.TypeInto, ...) against the real Element.
func TestSkillGoElementMethodsExist(t *testing.T) {
	block := langBlock(t, "go")
	methods := documentedMethods(block, "el")
	// Floor guards against a vacuous pass: the skill documents four chained
	// actions, so a parse finding fewer has silently stopped checking them.
	if len(methods) < 4 {
		t.Fatalf("only found %d documented el.X() methods (%v) — the parse is probably broken, "+
			"which would make this test vacuous", len(methods), methods)
	}
	typ := reflect.TypeOf(mobile.Element{})
	for _, name := range methods {
		if _, ok := typ.MethodByName(name); !ok {
			t.Errorf("agentskill.md documents el.%s(), which does not exist on mobile.Element", name)
		}
	}
}

// TestSkillGoErrorHelpersExist checks the error classifiers the Go section tells
// agents to use. These are package functions, so they are asserted by reference:
// a rename in platform-go breaks the build of this test.
func TestSkillGoErrorHelpersExist(t *testing.T) {
	helpers := map[string]func(error) bool{
		"mobile.IsElementNotFound": mobile.IsElementNotFound,
		"mobile.IsTimeout":         mobile.IsTimeout,
		"mobile.IsDeviceOffline":   mobile.IsDeviceOffline,
		"mobile.IsRetryable":       mobile.IsRetryable,
	}
	block := langBlock(t, "go")
	for name := range helpers {
		if !strings.Contains(block, name) {
			t.Errorf("the Go section no longer mentions %s — if the error guidance was "+
				"dropped, drop it from this test too, deliberately", name)
		}
	}
	// KeyEnter is the one key constant the skill names.
	if !strings.Contains(block, "mobile.KeyEnter") {
		t.Error("the Go section no longer mentions mobile.KeyEnter")
	}
	_ = mobile.KeyEnter
}

// TestSkillTeachesSemanticSelectors pins the rule that matters most for output
// quality: coordinates are only true for the screen you explored on, and the
// script runs later against a different phone from the pool.
func TestSkillTeachesSemanticSelectors(t *testing.T) {
	for _, want := range []string{
		"semantic selectors",
		"--query",
	} {
		if !strings.Contains(strings.ToLower(agentSkillBody), strings.ToLower(want)) {
			t.Errorf("agentskill.md no longer teaches %q", want)
		}
	}
	// The rule needs its own section, not a passing mention buried in a list.
	if !strings.Contains(agentSkillBody, "## Rule: always use semantic selectors, never raw coordinates") {
		t.Error("the semantic-selector rule lost its dedicated section")
	}
}

// TestSkillAsksForLanguage pins the language-choice step: without it an agent
// defaults to whichever SDK appears first and the user never gets a say.
func TestSkillAsksForLanguage(t *testing.T) {
	if !strings.Contains(agentSkillBody, "Which SDK should I write this in") {
		t.Error("agentskill.md no longer tells the agent to ask which SDK to write")
	}
	for _, lang := range []string{"python", "go"} {
		langBlock(t, lang) // fatals if the block is missing
	}
}
