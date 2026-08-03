package cmd

import (
	"time"

	"github.com/spf13/pflag"
)

const (
	durationUnitsHelp   = "Units: ns, us/µs, ms, s, m, h."
	deliveryTimeoutHelp = "Maximum delivery wait, such as 30s, 1m30s, or 2m; applies only with --wait (default 1m). " + durationUnitsHelp
	visionTimeoutHelp   = "Maximum vision search time, such as 500ms, 15s, or 1m (default 10s). " + durationUnitsHelp
	ocrTimeoutHelp      = "Maximum OCR polling time, such as 500ms, 10s, or 1m; exceeding it exits 5 (default 10s). " + durationUnitsHelp
	startTimeoutHelp    = "Queued-phone wait in whole seconds, such as 30, 300, or 900; positive values are sent to the server, while 0 or negative values use the server default"
)

// documentedDurationVar registers a duration flag whose usage already states
// its default in a concise form, such as 1m rather than time.Duration's 1m0s.
// DefValue controls only the automatic help suffix; DurationVar has already
// assigned the real default to target.
func documentedDurationVar(flags *pflag.FlagSet, target *time.Duration, name string, value time.Duration, usage string) {
	flags.DurationVar(target, name, value, usage)
	flags.Lookup(name).DefValue = ""
}
