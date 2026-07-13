package cmd

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/axilioai/cli/internal/output"
	"github.com/axilioai/cli/internal/session"
	"github.com/axilioai/platform-go/drivers/mobile"
	"github.com/spf13/cobra"
)

// flagPhoneSession is the --session override for the phone verbs.
var flagPhoneSession string

func phoneCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "phone",
		Short: "Drive the current phone session: observe, find, tap, type, ...",
		Long: "Drive a phone leased with `axilio sessions start`. The verbs are thin " +
			"projections of the SDK's MobileDriver, so a session you explore here maps " +
			"1:1 onto SDK code. Use -o json to parse observe/find results.",
	}
	cmd.PersistentFlags().StringVar(&flagPhoneSession, "session", "", "Target session id (defaults to the current session)")
	cmd.AddCommand(
		phoneObserveCmd(), phoneFindCmd(), phoneFindTextCmd(),
		phoneTapCmd(), phoneLongPressCmd(), phoneSwipeCmd(),
		phoneTypeCmd(), phoneKeyCmd(), phoneScreenshotCmd(), phoneWaitForCmd(),
	)
	return cmd
}

// currentDriver resolves the session to drive and opens a MobileDriver on its
// control URL. The control URL is captured at `sessions start` (it is minted
// only then), so the CLI can only drive a session it started.
func currentDriver() (*mobile.MobileDriver, error) {
	s, ok := session.Load()
	if !ok {
		return nil, fmt.Errorf("no current session; run `axilio sessions start` first")
	}
	if flagPhoneSession != "" && !s.Matches(flagPhoneSession) {
		return nil, fmt.Errorf(
			"session %q is not the current session (%s); this CLI can only drive a session it started",
			flagPhoneSession, s.SessionID)
	}
	if s.ControlURL == "" {
		return nil, fmt.Errorf("current session has no control URL; re-run `axilio sessions start`")
	}
	return mobile.ConnectRemote(s.ControlURL), nil
}

func visionOpts(engine, model string) []mobile.CallOption {
	var opts []mobile.CallOption
	if engine != "" {
		opts = append(opts, mobile.WithOCREngine(engine))
	}
	if model != "" {
		opts = append(opts, mobile.WithModel(model))
	}
	return opts
}

func elementKV(el mobile.Element) [][2]string {
	return [][2]string{
		{"Text", el.Text},
		{"Center", fmt.Sprintf("%d,%d", el.Center.X, el.Center.Y)},
		{"BBox", fmt.Sprintf("%d,%d %dx%d", el.BBox.X, el.BBox.Y, el.BBox.Width, el.BBox.Height)},
		{"Confidence", fmt.Sprintf("%.2f", el.Confidence)},
		{"Source", string(el.Source)},
	}
}

func phoneObserveCmd() *cobra.Command {
	var engine string
	cmd := &cobra.Command{
		Use:   "observe",
		Short: "Capture the screen: text + icon elements with coordinates.",
		RunE: func(_ *cobra.Command, _ []string) error {
			d, err := currentDriver()
			if err != nil {
				return err
			}
			defer d.Close()
			screen, err := d.Observe(visionOpts(engine, "")...)
			if err != nil {
				return err
			}
			p := printer()
			p.Emit(screen, func() {
				rows := [][]string{{"TEXT", "X", "Y", "CONF"}}
				for _, t := range screen.Texts {
					rows = append(rows, []string{t.Text, strconv.Itoa(t.Center.X), strconv.Itoa(t.Center.Y), fmt.Sprintf("%.2f", t.Confidence)})
				}
				output.Table(rows)
				p.Note("%d texts, %d icons  %dx%d", len(screen.Texts), len(screen.Icons), screen.Width, screen.Height)
			})
			return nil
		},
	}
	cmd.Flags().StringVar(&engine, "ocr-engine", "", "OCR engine (free|premium)")
	return cmd
}

func phoneFindCmd() *cobra.Command {
	var engine, model string
	var timeout time.Duration
	cmd := &cobra.Command{
		Use:   "find <query>",
		Short: "Locate an element by natural-language query (vision).",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			d, err := currentDriver()
			if err != nil {
				return err
			}
			defer d.Close()
			opts := visionOpts(engine, model)
			if timeout > 0 {
				opts = append(opts, mobile.WithTimeout(timeout))
			}
			el, err := d.Find(args[0], opts...)
			if err != nil {
				return err
			}
			printer().Emit(el, func() { output.KV(elementKV(*el)) })
			return nil
		},
	}
	cmd.Flags().StringVar(&engine, "ocr-engine", "", "OCR engine (free|premium)")
	cmd.Flags().StringVar(&model, "model", "", "Vision model override")
	cmd.Flags().DurationVar(&timeout, "timeout", 0, "Deadline, e.g. 15s")
	return cmd
}

func phoneFindTextCmd() *cobra.Command {
	var exact bool
	cmd := &cobra.Command{
		Use:   "find-text <text>",
		Short: "First OCR element matching text (nil if none).",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			d, err := currentDriver()
			if err != nil {
				return err
			}
			defer d.Close()
			el, err := d.FindText(args[0], exact)
			if err != nil {
				return err
			}
			printer().Emit(el, func() {
				if el == nil {
					fmt.Println("No match.")
					return
				}
				output.KV(elementKV(*el))
			})
			return nil
		},
	}
	cmd.Flags().BoolVar(&exact, "exact", false, "Exact match instead of case-insensitive substring")
	return cmd
}

func phoneTapCmd() *cobra.Command {
	var query, engine, model string
	cmd := &cobra.Command{
		Use:   "tap [x y]",
		Short: "Tap at coordinates, or at a natural-language target with --query.",
		Args:  cobra.MaximumNArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			d, err := currentDriver()
			if err != nil {
				return err
			}
			defer d.Close()
			if query != "" {
				el, err := d.Find(query, visionOpts(engine, model)...)
				if err != nil {
					return err
				}
				if err := el.Tap(); err != nil {
					return err
				}
				printer().Note("Tapped %q at %d,%d", query, el.Center.X, el.Center.Y)
				return nil
			}
			c, err := coordsArg(args)
			if err != nil {
				return err
			}
			if err := d.Tap(c); err != nil {
				return err
			}
			printer().Note("Tapped %d,%d", c.X, c.Y)
			return nil
		},
	}
	cmd.Flags().StringVar(&query, "query", "", "Natural-language target (routed through vision)")
	cmd.Flags().StringVar(&engine, "ocr-engine", "", "OCR engine for --query")
	cmd.Flags().StringVar(&model, "model", "", "Vision model for --query")
	return cmd
}

func phoneLongPressCmd() *cobra.Command {
	var durationMs int
	cmd := &cobra.Command{
		Use:   "long-press <x> <y>",
		Short: "Press and hold at coordinates.",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			d, err := currentDriver()
			if err != nil {
				return err
			}
			defer d.Close()
			c, err := coordsArg(args)
			if err != nil {
				return err
			}
			if err := d.LongPress(c, durationMs); err != nil {
				return err
			}
			printer().Note("Long-pressed %d,%d for %dms", c.X, c.Y, durationMs)
			return nil
		},
	}
	cmd.Flags().IntVar(&durationMs, "duration-ms", 800, "Hold duration in milliseconds")
	return cmd
}

func phoneSwipeCmd() *cobra.Command {
	var durationMs int
	cmd := &cobra.Command{
		Use:   "swipe <x1> <y1> <x2> <y2>",
		Short: "Swipe from one point to another.",
		Args:  cobra.ExactArgs(4),
		RunE: func(_ *cobra.Command, args []string) error {
			d, err := currentDriver()
			if err != nil {
				return err
			}
			defer d.Close()
			nums, err := intArgs(args)
			if err != nil {
				return err
			}
			start := mobile.Coords{X: nums[0], Y: nums[1]}
			end := mobile.Coords{X: nums[2], Y: nums[3]}
			if err := d.Swipe(start, end, durationMs); err != nil {
				return err
			}
			printer().Note("Swiped %d,%d -> %d,%d", start.X, start.Y, end.X, end.Y)
			return nil
		},
	}
	cmd.Flags().IntVar(&durationMs, "duration-ms", 300, "Swipe duration in milliseconds")
	return cmd
}

func phoneTypeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "type <text>",
		Short: "Type a string of text.",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			d, err := currentDriver()
			if err != nil {
				return err
			}
			defer d.Close()
			if err := d.TypeText(args[0]); err != nil {
				return err
			}
			printer().Note("Typed %q", args[0])
			return nil
		},
	}
}

func phoneKeyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "key <name>",
		Short: "Press a named key, e.g. `enter`.",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			d, err := currentDriver()
			if err != nil {
				return err
			}
			defer d.Close()
			if err := d.KeyPress(args[0]); err != nil {
				return err
			}
			printer().Note("Pressed %s", args[0])
			return nil
		},
	}
}

func phoneScreenshotCmd() *cobra.Command {
	var out string
	cmd := &cobra.Command{
		Use:   "screenshot",
		Short: "Capture the screen as a PNG file.",
		RunE: func(_ *cobra.Command, _ []string) error {
			d, err := currentDriver()
			if err != nil {
				return err
			}
			defer d.Close()
			png, err := d.Screenshot()
			if err != nil {
				return err
			}
			if err := os.WriteFile(out, png, 0o644); err != nil {
				return err
			}
			printer().Note("Wrote %s (%d bytes)", out, len(png))
			return nil
		},
	}
	cmd.Flags().StringVar(&out, "out", "screenshot.png", "Output PNG path")
	return cmd
}

func phoneWaitForCmd() *cobra.Command {
	var (
		timeout time.Duration
		exact   bool
		gone    bool
	)
	cmd := &cobra.Command{
		Use:   "wait-for <text>",
		Short: "Poll until text appears (or disappears with --gone).",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			d, err := currentDriver()
			if err != nil {
				return err
			}
			defer d.Close()
			if gone {
				if err := d.WaitUntilGone(args[0], timeout, exact); err != nil {
					return err
				}
				printer().Note("%q gone", args[0])
				return nil
			}
			el, err := d.WaitForText(args[0], timeout, exact)
			if err != nil {
				return err
			}
			printer().Emit(el, func() { output.KV(elementKV(*el)) })
			return nil
		},
	}
	cmd.Flags().DurationVar(&timeout, "timeout", 10*time.Second, "How long to poll")
	cmd.Flags().BoolVar(&exact, "exact", false, "Exact match instead of substring")
	cmd.Flags().BoolVar(&gone, "gone", false, "Wait until the text disappears instead")
	return cmd
}

// coordsArg parses exactly two positional ints into Coords.
func coordsArg(args []string) (mobile.Coords, error) {
	if len(args) != 2 {
		return mobile.Coords{}, fmt.Errorf("need x and y (or use --query)")
	}
	nums, err := intArgs(args)
	if err != nil {
		return mobile.Coords{}, err
	}
	return mobile.Coords{X: nums[0], Y: nums[1]}, nil
}

func intArgs(args []string) ([]int, error) {
	out := make([]int, len(args))
	for i, a := range args {
		n, err := strconv.Atoi(a)
		if err != nil {
			return nil, fmt.Errorf("%q is not an integer", a)
		}
		out[i] = n
	}
	return out, nil
}
