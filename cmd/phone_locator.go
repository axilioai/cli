package cmd

import (
	"fmt"
	"strings"

	"github.com/axilioai/cli/internal/exit"
	"github.com/axilioai/platform-go/drivers/mobile"
	"github.com/spf13/pflag"
)

// _strategyValues are the --strategy values, in the order help lists them.
var _strategyValues = []string{
	string(mobile.StrategyAuto), string(mobile.StrategyVision), string(mobile.StrategyAccessibility),
}

// locatorFlags are the selector flags the locator verbs (find, tap, wait-for)
// share. They map one to one onto the SDK's locator options, so a target
// explored here transfers directly into Go or Python code. Every set
// selector must match (AND).
type locatorFlags struct {
	text, query, role, name, id string
	exact                       bool
	strategy                    string
	engine, model               string
}

// registerTree adds the accessibility-tree selectors and --strategy. They
// need the session's accessibility tree; without it the phone answers that
// the strategy is unavailable.
func (f *locatorFlags) registerTree(fs *pflag.FlagSet) {
	fs.StringVar(&f.role, "role", "", "Accessibility role, e.g. button or textbox; needs accessibility mode")
	fs.StringVar(&f.name, "name", "", "Accessible name, e.g. \"Log in\"; needs accessibility mode")
	fs.StringVar(&f.id, "id", "", "Android resource id, e.g. com.example.app:id/login; needs accessibility mode")
	fs.StringVar(&f.strategy, "strategy", "", "Resolver: auto, vision, or accessibility; omitted uses auto (the tree when on, else vision)")
}

// hasSelector reports whether any selector flag (or positional text) is set.
func (f *locatorFlags) hasSelector() bool {
	return f.text != "" || f.query != "" || f.role != "" || f.name != "" || f.id != ""
}

// build turns the flags into a locator. It is a usage error to pass no
// selector at all, or an unknown --strategy.
func (f *locatorFlags) build(d *mobile.MobileDriver) (*mobile.Locator, error) {
	if !f.hasSelector() {
		return nil, exit.Usagef("pass a target: a query, text, --role, --name, or --id")
	}
	var opts []mobile.LocatorOption
	add := func(v string, opt func(string) mobile.LocatorOption) {
		if v != "" {
			opts = append(opts, opt(v))
		}
	}
	add(f.text, mobile.Text)
	add(f.query, mobile.Query)
	add(f.role, mobile.Role)
	add(f.name, mobile.Name)
	add(f.id, mobile.ID)
	add(f.engine, mobile.OCREngine)
	add(f.model, mobile.Model)
	if f.exact {
		opts = append(opts, mobile.Exact())
	}
	if f.strategy != "" {
		s, err := oneOf("--strategy", f.strategy, _strategyValues)
		if err != nil {
			return nil, err
		}
		opts = append(opts, mobile.Strategy(mobile.LocatorStrategy(s)))
	}
	return d.Locator(opts...), nil
}

// describe renders the target for a human acknowledgment, e.g.
// `button "Log in"` or `"the search box"`.
func (f *locatorFlags) describe() string {
	var parts []string
	if f.role != "" {
		parts = append(parts, f.role)
	}
	for _, v := range []string{f.name, f.text, f.query} {
		if v != "" {
			parts = append(parts, fmt.Sprintf("%q", v))
		}
	}
	if f.id != "" {
		parts = append(parts, "#"+f.id)
	}
	return strings.Join(parts, " ")
}

// fields renders the set selectors for a JSON result, under the same names
// as the flags.
func (f *locatorFlags) fields(out map[string]any) map[string]any {
	for k, v := range map[string]string{
		"text": f.text, "query": f.query, "role": f.role, "name": f.name, "id": f.id, "strategy": f.strategy,
	} {
		if v != "" {
			out[k] = v
		}
	}
	return out
}
