package cmd

import (
	"strings"
	"testing"

	"github.com/axilioai/cli/internal/exit"
)

// The screenshot -> eyeball -> tap-coordinates loop is what any agent does by
// default: hand a vision model an image and it reasons in pixels. These tests pin
// the guardrails that make the semantic path the path of least resistance, so a
// later refactor can't quietly restore the old default.
//
// They assert argument handling only — reaching the driver needs a live session.
// That is the layer being changed: whether bare coordinates are reachable at all.

func TestBareCoordinatesAreAUsageError(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"tap", []string{"tap", "540", "1200"}},
		{"long-press", []string{"long-press", "540", "1200"}},
		{"swipe", []string{"swipe", "540", "1500", "540", "500"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := runPhoneArgs(t, c.args)
			if err == nil {
				t.Fatalf("%s accepted bare coordinates; they must require --raw", c.name)
			}
			if exit.Classify(err) != exit.Usage {
				t.Fatalf("%s: want a usage error, got %v", c.name, err)
			}
			// The error is the teaching moment — it has to name the alternative,
			// not just reject the input.
			if !strings.Contains(err.Error(), "query") {
				t.Errorf("%s: the error must point at the semantic alternative, got: %v", c.name, err)
			}
		})
	}
}

// Mixing the two forms is ambiguous about intent, so it's rejected rather than
// silently preferring one.
func TestQueryAndRawAreMutuallyExclusive(t *testing.T) {
	cases := [][]string{
		{"tap", "--query", "the search box", "--raw", "540", "1200"},
		{"long-press", "--query", "the message", "--raw", "540", "1200"},
		{"swipe", "--from-query", "a", "--to-query", "b", "--raw", "1", "2", "3", "4"},
	}
	for _, args := range cases {
		t.Run(args[0], func(t *testing.T) {
			err := runPhoneArgs(t, args)
			if err == nil || exit.Classify(err) != exit.Usage {
				t.Fatalf("%v: want a usage error for mixing --query and --raw, got %v", args, err)
			}
		})
	}
}

// A half-specified semantic swipe is a mistake worth catching: silently swiping
// from an element to nowhere would look like it worked.
func TestSemanticSwipeNeedsBothEnds(t *testing.T) {
	for _, args := range [][]string{
		{"swipe", "--from-query", "the photo"},
		{"swipe", "--to-query", "the trash icon"},
	} {
		err := runPhoneArgs(t, args)
		if err == nil || exit.Classify(err) != exit.Usage {
			t.Fatalf("%v: want a usage error, got %v", args, err)
		}
		if !strings.Contains(err.Error(), "--raw") {
			t.Errorf("%v: the error should offer --raw for gestures, got: %v", args, err)
		}
	}
}

// Every action verb must be reachable semantically. This is the one that caused
// the original bug: long-press and swipe accepted coordinates only, so "never use
// raw coordinates" was unfollowable and an agent using them was simply obeying
// the tool.
func TestEveryActionVerbHasASemanticForm(t *testing.T) {
	root := phoneCmd()
	want := map[string]string{
		"tap":        "query",
		"long-press": "query",
		"swipe":      "from-query",
	}
	for verb, flag := range want {
		var found bool
		for _, c := range root.Commands() {
			if strings.Fields(c.Use)[0] != verb {
				continue
			}
			found = true
			if c.Flags().Lookup(flag) == nil {
				t.Errorf("`phone %s` has no --%s: raw coordinates would be the only option, "+
					"which makes the semantic rule unfollowable", verb, flag)
			}
			if c.Flags().Lookup("raw") == nil {
				t.Errorf("`phone %s` has no --raw: coordinates must be an explicit opt-in", verb)
			}
		}
		if !found {
			t.Errorf("`phone %s` not registered", verb)
		}
	}
}

// runPhoneArgs executes the phone command tree for its argument handling and
// returns the resulting error. Anything that parses cleanly then fails to reach a
// session is reported as such by the caller's assertions.
func runPhoneArgs(t *testing.T, args []string) error {
	t.Helper()
	cmd := phoneCmd()
	cmd.SetArgs(args)
	cmd.SetOut(discard{})
	cmd.SetErr(discard{})
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	return cmd.Execute()
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }
