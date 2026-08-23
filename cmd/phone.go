package cmd

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/axilioai/cli/internal/exit"
	"github.com/axilioai/cli/internal/session"
	"github.com/axilioai/platform-go/drivers/mobile"
	"github.com/spf13/cobra"
)

// flagPhoneSession is the --session override for the phone verbs.
var flagPhoneSession string

func phoneCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "phone",
		Short: "Observe and control the selected session's phone.",
		Long: "Drive a phone session started with `axilio sessions start`.\n\n" +
			"A reliable loop is " +
			"observe the screen, find or semantically target an element, act, then " +
			"observe again to verify.\n\n" +
			"Available verbs are observe, find, find-text, find-all-text, " +
			"tap, long-press, swipe, type, key, screenshot, wait-for, and send. " +
			"Every successful verb emits a structured result with -o json.\n\n" +
			"Session selection precedence is --session, AXILIO_SESSION, the only locally " +
			"saved session, the most recently started session, then an ambiguity error. " +
			"Phone command names match the Axilio SDKs, so an interaction explored in " +
			"the CLI can be transferred directly into Python or Go code.\n\n" +
			"Running `axilio phone` without a subcommand is equivalent to " +
			"`axilio phone --help`: it only displays this help and does not observe " +
			"or control a phone. Phone and global flags shown here therefore have no " +
			"effect. Pass flags to a phone subcommand instead.",
	}
	cmd.PersistentFlags().StringVar(&flagPhoneSession, "session", "", "Session ID; overrides AXILIO_SESSION and automatic session selection")
	cmd.AddCommand(
		phoneObserveCmd(), phoneFindCmd(), phoneFindTextCmd(), phoneFindAllTextCmd(),
		phoneTapCmd(), phoneLongPressCmd(), phoneSwipeCmd(),
		phoneTypeCmd(), phoneKeyCmd(), phoneScreenshotCmd(), phoneWaitForCmd(),
		phoneSendCmd(),
	)
	return cmd
}

// currentDriver resolves which lease to drive (precedence: --session flag >
// AXILIO_SESSION env > sole active lease > current pointer) and opens a
// MobileDriver on its control URL. The control URL is captured at
// `sessions start` (it is minted only then).
func currentDriver() (*mobile.MobileDriver, error) {
	s, err := session.Resolve(flagPhoneSession)
	if err != nil {
		return nil, err
	}
	if s.ControlURL == "" {
		return nil, fmt.Errorf("session %s has no control URL; re-run `axilio sessions start`", s.SessionID)
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
		Long: "Capture and analyze the selected phone's current screen. OCR uses the " +
			"free engine when --ocr-engine is omitted. Table output lists recognized " +
			"text with center coordinates and confidence, then summarizes icons and " +
			"screen dimensions. JSON returns the complete screen object, including " +
			"texts, icons, dimensions, screen hash, and capture time.",
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
			return p.Emit(screen, func() {
				rows := [][]string{{"TEXT", "X", "Y", "CONF"}}
				for _, t := range screen.Texts {
					rows = append(rows, []string{t.Text, strconv.Itoa(t.Center.X), strconv.Itoa(t.Center.Y), fmt.Sprintf("%.2f", t.Confidence)})
				}
				p.Table(rows)
				p.Note("%d texts, %d icons  %dx%d", len(screen.Texts), len(screen.Icons), screen.Width, screen.Height)
			})
		},
	}
	cmd.Flags().StringVar(&engine, "ocr-engine", "", "OCR engine: free or premium; omitted uses free")
	return cmd
}

func phoneFindCmd() *cobra.Command {
	var engine, model string
	var timeout time.Duration
	cmd := &cobra.Command{
		Use:   "find <query>",
		Short: "Locate an element by natural-language query (vision).",
		Long: "Locate one visible element from a natural-language description and " +
			"return its text, center, bounding box, confidence, and source. The OCR " +
			"engine defaults to free, the vision model is selected by the server, " +
			"and the effective deadline is 10 seconds when --timeout is omitted. A " +
			"missing target is an error; use `find-text` for a successful empty result.",
		Args: cobra.ExactArgs(1),
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
			p := printer()
			return p.Emit(el, func() { p.KV(elementKV(*el)) })
		},
	}
	cmd.Flags().StringVar(&engine, "ocr-engine", "", "OCR engine: free or premium; omitted uses free")
	cmd.Flags().StringVar(&model, "model", "", "Vision model override; omitted lets the server select the model")
	documentedDurationVar(cmd.Flags(), &timeout, "timeout", 10*time.Second, visionTimeoutHelp)
	return cmd
}

func phoneFindTextCmd() *cobra.Command {
	var exact bool
	cmd := &cobra.Command{
		Use:   "find-text <text>",
		Short: "Return the first OCR text match, or an empty successful result.",
		Long: "Search OCR text without semantic vision. By default, match a " +
			"case-insensitive substring and return the first element. --exact uses " +
			"a case-sensitive exact match. No match is successful: table output " +
			"prints `No match.` and JSON output is null rather than a not-found error.",
		Args: cobra.ExactArgs(1),
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
			p := printer()
			return p.Emit(el, func() {
				if el == nil {
					p.Result("No match.")
					return
				}
				p.KV(elementKV(*el))
			})
		},
	}
	cmd.Flags().BoolVar(&exact, "exact", false, "Require a case-sensitive exact match instead of case-insensitive substring")
	return cmd
}

func phoneFindAllTextCmd() *cobra.Command {
	var pattern, engine string
	cmd := &cobra.Command{
		Use:   "find-all-text [contains]",
		Short: "Return every OCR text match, or all texts with no criteria.",
		Long: "Return every OCR element matching the criteria. The positional " +
			"argument is a case-insensitive substring; --pattern is a Go regular " +
			"expression instead. They are mutually exclusive. With neither, every " +
			"text element on the screen is returned. No match is successful: table " +
			"output prints `No matches.` and JSON output is an empty array.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			contains := ""
			if len(args) == 1 {
				contains = args[0]
			}
			if contains != "" && pattern != "" {
				return exit.Usagef("pass at most one of <contains> / --pattern")
			}
			d, err := currentDriver()
			if err != nil {
				return err
			}
			defer d.Close()
			els, err := d.FindAllText(contains, pattern, visionOpts(engine, "")...)
			if err != nil {
				return err
			}
			// Non-nil so an empty result serializes as [] in JSON output.
			if els == nil {
				els = []mobile.Element{}
			}
			p := printer()
			return p.Emit(els, func() {
				if len(els) == 0 {
					p.Result("No matches.")
					return
				}
				rows := [][]string{{"TEXT", "X", "Y", "CONF"}}
				for _, el := range els {
					rows = append(rows, []string{el.Text, strconv.Itoa(el.Center.X), strconv.Itoa(el.Center.Y), fmt.Sprintf("%.2f", el.Confidence)})
				}
				p.Table(rows)
			})
		},
	}
	cmd.Flags().StringVar(&pattern, "pattern", "", "Go regular expression to match instead of a substring")
	cmd.Flags().StringVar(&engine, "ocr-engine", "", "OCR engine: free or premium; omitted uses free")
	return cmd
}

func phoneTapCmd() *cobra.Command {
	var query, engine, model string
	cmd := &cobra.Command{
		Use:   "tap [x y]",
		Short: "Tap at coordinates, or at a natural-language target with --query.",
		Long: "Perform a tap action on the selected phone.\n\n" +
			"The coordinate form takes x and y as frame-space pixels, with (0,0) " +
			"at the screen's top-left.\n\n" +
			"Use --query to find an element by natural-language description and tap " +
			"its center. If --query and coordinates are both provided, --query takes " +
			"precedence.\n\n" +
			"Session selection precedence is --session, AXILIO_SESSION, the only " +
			"locally saved session, the most recently started session, then an " +
			"ambiguity error.",
		Args: cobra.MaximumNArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			d, err := currentDriver()
			if err != nil {
				return err
			}
			defer d.Close()
			p := printer()
			if query != "" {
				el, err := d.Find(query, visionOpts(engine, model)...)
				if err != nil {
					return err
				}
				if err := el.Tap(); err != nil {
					return err
				}
				return p.Emit(map[string]any{"action": "tap", "query": query, "x": el.Center.X, "y": el.Center.Y}, func() {
					p.Ack("Tapped %q at %d,%d", query, el.Center.X, el.Center.Y)
				})
			}
			c, err := coordsArg(args)
			if err != nil {
				return err
			}
			if err := d.Tap(c); err != nil {
				return err
			}
			return p.Emit(map[string]any{"action": "tap", "x": c.X, "y": c.Y}, func() {
				p.Ack("Tapped %d,%d", c.X, c.Y)
			})
		},
	}
	cmd.Flags().StringVar(&query, "query", "", "Recommended natural-language target; vision finds it and taps its center")
	cmd.Flags().StringVar(&engine, "ocr-engine", "", "OCR engine for --query only: free or premium; omitted uses free")
	cmd.Flags().StringVar(&model, "model", "", "Vision model for --query only; omitted lets the server select")
	return cmd
}

func phoneLongPressCmd() *cobra.Command {
	var durationMs int
	cmd := &cobra.Command{
		Use:   "long-press <x> <y>",
		Short: "Press and hold at coordinates.",
		Long: "Press and hold at frame-space pixel coordinates, with (0,0) at the " +
			"screen's top-left. This command is coordinate-only; use observe or find " +
			"to inspect the current frame before choosing a point. The default hold " +
			"duration is 800 milliseconds.",
		Args: cobra.ExactArgs(2),
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
			p := printer()
			return p.Emit(map[string]any{"action": "long_press", "x": c.X, "y": c.Y, "duration_ms": durationMs}, func() {
				p.Ack("Long-pressed %d,%d for %dms", c.X, c.Y, durationMs)
			})
		},
	}
	cmd.Flags().IntVar(&durationMs, "duration-ms", 800, "How long to hold the coordinate, in milliseconds")
	return cmd
}

func phoneSwipeCmd() *cobra.Command {
	var durationMs int
	cmd := &cobra.Command{
		Use:   "swipe <x1> <y1> <x2> <y2>",
		Short: "Swipe from one point to another.",
		Long: "Swipe between two frame-space pixel coordinates, with (0,0) at the " +
			"screen's top-left. This command is coordinate-only; use observe to " +
			"inspect the current frame. The default gesture duration is 300 milliseconds.",
		Args: cobra.ExactArgs(4),
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
			p := printer()
			return p.Emit(
				map[string]any{"action": "swipe", "x1": start.X, "y1": start.Y, "x2": end.X, "y2": end.Y, "duration_ms": durationMs},
				func() { p.Ack("Swiped %d,%d -> %d,%d", start.X, start.Y, end.X, end.Y) },
			)
		},
	}
	cmd.Flags().IntVar(&durationMs, "duration-ms", 300, "How long the swipe gesture takes, in milliseconds")
	return cmd
}

func phoneTypeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "type <text>",
		Short: "Type a string of text.",
		Long: "Type text into the focused field on the selected phone.\n\n" +
			"Enclose text in quotes when it contains spaces or shell-special characters. " +
			"Text is entered through a US-layout keyboard. Printable ASCII characters " +
			"are supported; emoji and other non-ASCII characters are silently skipped.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			d, err := currentDriver()
			if err != nil {
				return err
			}
			defer d.Close()
			if err := d.TypeText(args[0]); err != nil {
				return err
			}
			p := printer()
			return p.Emit(map[string]any{"action": "type", "text": args[0]}, func() {
				p.Ack("Typed %q", args[0])
			})
		},
	}
}

func phoneKeyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "key <name>",
		Short: "Press a named key. Currently supported: enter.",
		Long: "Press a named key on the selected phone. The name is passed through " +
			"to the phone, which accepts the supported named-key set and rejects " +
			"anything else. The set is currently just `enter` (submits forms / fires " +
			"the on-screen keyboard's Go or Search action) and grows in lockstep " +
			"with the device side, so new names work here without a CLI update.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			d, err := currentDriver()
			if err != nil {
				return err
			}
			defer d.Close()
			if err := d.KeyPress(args[0]); err != nil {
				return err
			}
			p := printer()
			return p.Emit(map[string]any{"action": "key", "key": args[0]}, func() {
				p.Ack("Pressed %s", args[0])
			})
		},
	}
}

func phoneScreenshotCmd() *cobra.Command {
	var out string
	cmd := &cobra.Command{
		Use:   "screenshot",
		Short: "Capture the screen as a PNG file.",
		Long: "Capture the selected phone's screen as PNG bytes and write them to " +
			"--out. The default destination is screenshot.png in the current " +
			"directory. If the destination already exists, its contents are overwritten " +
			"without confirmation; the CLI does not create a backup. On " +
			"success the human result reports the path and byte count.",
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
			p := printer()
			return p.Emit(map[string]any{"action": "screenshot", "path": out, "bytes": len(png)}, func() {
				p.Ack("Wrote %s (%d bytes)", out, len(png))
			})
		},
	}
	cmd.Flags().StringVar(&out, "out", "screenshot.png", "PNG path to create; overwrite existing contents without confirmation")
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
		Long: "Poll OCR until text appears, or until it disappears with --gone. The " +
			"default match is a case-insensitive substring; --exact requires a " +
			"case-sensitive exact match. The default timeout is 10 seconds. A timeout " +
			"returns the CLI timeout exit code (5). Waiting for presence returns the " +
			"matching element; waiting for absence is action-only.",
		Args: cobra.ExactArgs(1),
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
				p := printer()
				return p.Emit(map[string]any{"action": "wait_for", "text": args[0], "gone": true}, func() {
					p.Ack("%q gone", args[0])
				})
			}
			el, err := d.WaitForText(args[0], timeout, exact)
			if err != nil {
				return err
			}
			p := printer()
			return p.Emit(el, func() { p.KV(elementKV(*el)) })
		},
	}
	documentedDurationVar(cmd.Flags(), &timeout, "timeout", 10*time.Second, ocrTimeoutHelp)
	cmd.Flags().BoolVar(&exact, "exact", false, "Require a case-sensitive exact match instead of substring")
	cmd.Flags().BoolVar(&gone, "gone", false, "Wait for the text to disappear instead of appear")
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
