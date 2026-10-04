package cmd

import (
	"fmt"
	"strings"

	"github.com/axilioai/cli/internal/exit"
	"github.com/axilioai/platform-go/drivers/mobile"
	"github.com/spf13/cobra"
)

func phoneTreeCmd() *cobra.Command {
	var (
		window string
		all    bool
		depth  int
	)
	cmd := &cobra.Command{
		Use:   "tree",
		Short: "Print the phone's accessibility tree.",
		Long: "Print the accessibility tree of the selected phone, one window at a " +
			"time, as an indented outline: each node's role, name, resource id, " +
			"center, and node id. The session must have accessibility mode on " +
			"(`sessions start` turns it on by default where the phone supports it; " +
			"see `phone accessibility status`); otherwise the phone answers that the " +
			"strategy is unavailable and the command exits 2. Layout-only nodes are " +
			"dropped unless --all is given. --window limits the tree to one window " +
			"and --depth to that many levels below each window root. JSON returns " +
			"the complete snapshot: nodes, windows, and capture time. The role, name, " +
			"and resource id shown here are what `phone tap --role --name --id` match.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var opts []mobile.SnapshotOption
			if window != "" {
				opts = append(opts, mobile.WithWindow(window))
			}
			if all {
				opts = append(opts, mobile.WithInterestingOnly(false))
			}
			if cmd.Flags().Changed("depth") {
				if depth < 0 {
					return exit.Usagef("--depth must be >= 0 (got %d)", depth)
				}
				opts = append(opts, mobile.WithDepth(depth))
			}
			d, err := currentDriver()
			if err != nil {
				return err
			}
			defer d.Close()
			tree, err := d.Accessibility().Snapshot(opts...)
			if err != nil {
				return err
			}
			p := printer()
			return p.Emit(tree, func() {
				lines := treeOutline(tree)
				if len(lines) == 0 {
					p.Result("No nodes.")
					return
				}
				p.Result("%s", strings.Join(lines, "\n"))
				p.Note("%d nodes, %d windows", len(tree.Nodes), len(tree.Windows))
			})
		},
	}
	cmd.Flags().StringVar(&window, "window", "", "Only this window id, as the outline's window lines list it")
	cmd.Flags().BoolVar(&all, "all", false, "Include layout-only nodes the phone drops by default")
	cmd.Flags().IntVar(&depth, "depth", 0, "Levels below each window root; omitted prints the whole tree")
	return cmd
}

// treeOutline renders a snapshot as one line per window followed by its
// nodes, indented by depth. A node's children follow it in document order.
// Nodes whose parent is not in the snapshot (a depth or window cut) start
// their own subtree, so no node is ever dropped from the outline.
func treeOutline(tree *mobile.AXTree) []string {
	byID := make(map[string]*mobile.AXNode, len(tree.Nodes))
	for i := range tree.Nodes {
		byID[tree.Nodes[i].NodeID] = &tree.Nodes[i]
	}
	seen := make(map[string]bool, len(tree.Nodes))
	var lines []string
	var walk func(n *mobile.AXNode, level int)
	walk = func(n *mobile.AXNode, level int) {
		if seen[n.NodeID] {
			return
		}
		seen[n.NodeID] = true
		lines = append(lines, strings.Repeat("  ", level)+nodeLine(n))
		for _, id := range n.ChildIDs {
			if c, ok := byID[id]; ok {
				walk(c, level+1)
			}
		}
	}
	isRoot := func(n *mobile.AXNode) bool {
		_, hasParent := byID[n.ParentID]
		return n.ParentID == "" || !hasParent
	}
	for _, w := range tree.Windows {
		lines = append(lines, windowLine(w))
		for i := range tree.Nodes {
			n := &tree.Nodes[i]
			if n.WindowID == w.WindowID && isRoot(n) {
				walk(n, 1)
			}
		}
	}
	// Nodes in a window the snapshot did not list, if any.
	for i := range tree.Nodes {
		if n := &tree.Nodes[i]; !seen[n.NodeID] && isRoot(n) {
			walk(n, 0)
		}
	}
	return lines
}

func windowLine(w mobile.AXWindow) string {
	parts := []string{fmt.Sprintf("window %s [%s]", w.WindowID, w.Type)}
	if w.App != "" {
		parts = append(parts, w.App)
	}
	if w.Title != "" {
		parts = append(parts, fmt.Sprintf("%q", w.Title))
	}
	if w.Focused {
		parts = append(parts, "focused")
	}
	return strings.Join(parts, "  ")
}

func nodeLine(n *mobile.AXNode) string {
	parts := []string{n.Role.String()}
	if name := n.Name.String(); name != "" {
		parts = append(parts, fmt.Sprintf("%q", name))
	}
	if v := n.Value.String(); v != "" {
		parts = append(parts, fmt.Sprintf("value=%q", v))
	}
	if n.Platform != nil && n.Platform.Android != nil && n.Platform.Android.ViewIDResourceName != "" {
		parts = append(parts, "#"+n.Platform.Android.ViewIDResourceName)
	}
	c := n.Bounds.Center()
	parts = append(parts, fmt.Sprintf("@%d,%d", c.X, c.Y), "["+n.NodeID+"]")
	return strings.Join(parts, " ")
}

func phoneAccessibilityCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "accessibility",
		Short: "Show or toggle accessibility mode for the selected session.",
		Long: "Show whether the selected session's accessibility tree is on, or turn " +
			"it on or off mid-session. With the tree on, `phone tree` reads it and " +
			"--role, --name, and --id select elements from it; while it is on, the " +
			"accessibility service is visible to apps on the phone. Choose the " +
			"session's starting state with the accessibility flags of " +
			"`sessions start`.\n\n" +
			"Running `axilio phone accessibility` without a subcommand is equivalent " +
			"to `axilio phone accessibility --help`.",
	}
	cmd.AddCommand(phoneAccessibilityStatusCmd(), phoneAccessibilityToggleCmd(true), phoneAccessibilityToggleCmd(false))
	return cmd
}

func phoneAccessibilityStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show whether the accessibility tree is on and whether it can be toggled.",
		Long: "Report whether the selected session's accessibility tree is on, and " +
			"whether this session can turn it on and off with `phone accessibility " +
			"enable` and `disable`. A phone that cannot provide the tree at all " +
			"answers with an unknown-operation error.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			d, err := currentDriver()
			if err != nil {
				return err
			}
			defer d.Close()
			st, err := d.Accessibility().State()
			if err != nil {
				return err
			}
			p := printer()
			return p.Emit(st, func() {
				p.KV([][2]string{
					{"Enabled", fmt.Sprintf("%t", st.Enabled)},
					{"Toggleable", fmt.Sprintf("%t", st.Toggleable)},
				})
			})
		},
	}
}

func phoneAccessibilityToggleCmd(on bool) *cobra.Command {
	use, verb, state := "disable", "Turn the accessibility tree off", "off"
	if on {
		use, verb, state = "enable", "Turn the accessibility tree on", "on"
	}
	return &cobra.Command{
		Use:   use,
		Short: verb + " for the selected session.",
		Long: verb + " for the selected session. The command returns once the " +
			"phone confirms the change. It works only where `phone accessibility " +
			"status` reports Toggleable true; elsewhere the phone answers with an " +
			"unknown-operation error.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			d, err := currentDriver()
			if err != nil {
				return err
			}
			defer d.Close()
			a := d.Accessibility()
			toggle := a.Disable
			if on {
				toggle = a.Enable
			}
			if err := toggle(); err != nil {
				return err
			}
			p := printer()
			return p.Emit(map[string]any{"action": "accessibility_" + use, "enabled": on}, func() {
				p.Ack("Accessibility %s.", state)
			})
		},
	}
}
