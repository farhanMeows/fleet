package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/farhanahmad/fleet/internal/client"
	"github.com/farhanahmad/fleet/internal/config"
	"github.com/farhanahmad/fleet/internal/event"
	"github.com/farhanahmad/fleet/internal/store"
	"github.com/farhanahmad/fleet/internal/tmuxdrv"
)

func newClient() (*client.Client, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	c := client.New(cfg)
	if err := c.EnsureDaemon(); err != nil {
		return nil, err
	}
	return c, nil
}

func addCmd() *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "add [path]",
		Short: "Register a project (defaults to the current directory)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			path := "."
			if len(args) == 1 {
				path = args[0]
			}
			abs, err := filepath.Abs(path)
			if err != nil {
				return err
			}
			if st, err := os.Stat(abs); err != nil || !st.IsDir() {
				return fmt.Errorf("%s is not a directory", abs)
			}
			if name == "" {
				name = store.SuggestName(abs)
			}
			c, err := newClient()
			if err != nil {
				return err
			}
			if err := c.AddProject(name, abs); err != nil {
				return err
			}
			fmt.Printf("added project %s → %s\n", name, abs)
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "project name (default: directory basename)")
	return cmd
}

func removeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <name>",
		Short: "Unregister a project",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			if err := c.RemoveProject(args[0]); err != nil {
				return err
			}
			fmt.Printf("removed project %s\n", args[0])
			return nil
		},
	}
}

func listCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List registered projects",
		RunE: func(_ *cobra.Command, _ []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			projects, err := c.Projects()
			if err != nil {
				return err
			}
			if len(projects) == 0 {
				fmt.Println("no projects registered — try: fleet add <path>")
				return nil
			}
			for _, p := range projects {
				fmt.Printf("%-24s %s\n", p.Name, p.Path)
			}
			return nil
		},
	}
}

func upCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "up",
		Short: "Launch (or attach to) the tmux mission-control session",
		RunE: func(_ *cobra.Command, _ []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			projects, err := c.Projects()
			if err != nil {
				return err
			}
			if len(projects) == 0 {
				return fmt.Errorf("no projects registered — run `fleet add <path>` first")
			}
			bin, err := os.Executable()
			if err != nil {
				return err
			}
			if err := tmuxdrv.Up(projects, bin); err != nil {
				return err
			}
			return tmuxdrv.Attach()
		},
	}
}

func replyCmd() *cobra.Command {
	var n int
	cmd := &cobra.Command{
		Use:   "reply <project>",
		Short: "Print the agent's most recent reply — read a dispatched task's result",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			replies, err := c.LastReply(args[0], n)
			if err != nil {
				return err
			}
			for i, r := range replies {
				if i > 0 {
					fmt.Println("\n---")
				}
				fmt.Println(r)
			}
			return nil
		},
	}
	cmd.Flags().IntVarP(&n, "count", "n", 1, "number of trailing replies to show")
	return cmd
}

func approveCmd() *cobra.Command {
	var deny bool
	cmd := &cobra.Command{
		Use:   "approve <project>",
		Short: "Answer a pending permission prompt remotely (opt-in: touch ~/.fleet/remote-approve)",
		Long: "Approves (or with --deny cancels) the permission dialog a project's agent is waiting on.\n" +
			"Refuses unless the daemon-tracked pending request is fresh, the agent is still waiting,\n" +
			"and the visible dialog matches the request. Approval is always single-shot (never\n" +
			"\"don't ask again\").",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			tool, summary, err := c.Approve(args[0], deny)
			if err != nil {
				return err
			}
			verb := "approved"
			if deny {
				verb = "denied"
			}
			fmt.Printf("%s on %s — %s: %s\n", verb, args[0], tool, summary)
			return nil
		},
	}
	cmd.Flags().BoolVar(&deny, "deny", false, "cancel the prompt instead of approving (sends Escape)")
	return cmd
}

func dispatchCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "dispatch <project> <prompt>",
		Short: "Send a prompt to a project's running agent without switching windows",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			project := args[0]
			prompt := strings.Join(args[1:], " ")
			c, err := newClient()
			if err != nil {
				return err
			}
			if err := c.Dispatch(project, prompt, force); err != nil {
				return err
			}
			fmt.Printf("dispatched to %s\n", project)
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "dispatch even if the agent is waiting for a permission decision")
	return cmd
}

func statusCmd() *cobra.Command {
	var watch bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show all projects and agent sessions",
		RunE: func(_ *cobra.Command, _ []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			if !watch {
				out, err := renderStatus(c)
				if err != nil {
					return err
				}
				fmt.Print(out)
				return nil
			}
			_, err = tea.NewProgram(newWatchModel(c), tea.WithAltScreen()).Run()
			return err
		},
	}
	cmd.Flags().BoolVar(&watch, "watch", false, "interactive dashboard (j/k move, enter jump, d dispatch, q quit)")
	return cmd
}

var stateStyle = map[string]struct{ icon, color string }{
	event.StateWorking:    {"●", "\x1b[33m"}, // yellow
	event.StateNeedsInput: {"⚠", "\x1b[31m"}, // red
	event.StateIdle:       {"✓", "\x1b[32m"}, // green
	"":                    {"○", "\x1b[2m"},  // dim
}

const reset = "\x1b[0m"

func renderStatus(c *client.Client) (string, error) {
	projects, err := c.Projects()
	if err != nil {
		return "", err
	}
	sessions, err := c.Sessions()
	if err != nil {
		return "", err
	}

	byProject := map[string][]store.Session{}
	for _, s := range sessions {
		byProject[s.Project] = append(byProject[s.Project], s)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "\x1b[1mFLEET\x1b[0m  %s\n\n", time.Now().Format("15:04:05"))
	fmt.Fprintf(&b, "  %-26s %-14s %-34s %s\n", "PROJECT", "STATE", "NOW", "AGE")
	fmt.Fprintf(&b, "  %s\n", strings.Repeat("─", 84))

	registered := map[string]bool{}
	for _, p := range projects {
		registered[p.Name] = true
		rows := byProject[p.Name]
		state, tool, summary, updatedAt := worstOf(rows)
		st := stateStyle[state]
		now := tool
		if summary != "" {
			now = tool + ": " + summary
		}
		fmt.Fprintf(&b, "  %s%s %-24s %-14s%s %-34s %s\n",
			st.color, st.icon, p.Name, stateLabel(state), reset, clip(now, 34), humanAge(updatedAt))
	}

	// Sessions running outside registered projects still deserve a row.
	var unregistered []string
	for name := range byProject {
		if !registered[name] {
			unregistered = append(unregistered, name)
		}
	}
	sort.Strings(unregistered)
	if len(unregistered) > 0 {
		fmt.Fprintf(&b, "\n  \x1b[2munregistered sessions:\x1b[0m\n")
		for _, name := range unregistered {
			state, tool, summary, updatedAt := worstOf(byProject[name])
			st := stateStyle[state]
			now := tool
			if summary != "" {
				now = tool + ": " + summary
			}
			fmt.Fprintf(&b, "  %s%s %-24s %-14s%s %-34s %s\n",
				st.color, st.icon, name, stateLabel(state), reset, clip(now, 34), humanAge(updatedAt))
		}
	}
	fmt.Fprintf(&b, "\n  \x1b[2mprefix+<n> jump · prefix+g dash · prefix+j picker · fleet dispatch <project> \"…\"\x1b[0m\n")
	return b.String(), nil
}

// worstOf picks the most attention-worthy session of a project.
func worstOf(rows []store.Session) (state, tool, summary string, updatedAt int64) {
	rank := map[string]int{event.StateIdle: 1, event.StateWorking: 2, event.StateNeedsInput: 3}
	var best *store.Session
	for i := range rows {
		if best == nil || rank[rows[i].State] > rank[best.State] {
			best = &rows[i]
		}
	}
	if best == nil {
		return "", "", "", 0
	}
	return best.State, best.Tool, best.Summary, best.UpdatedAt
}

func stateLabel(state string) string {
	switch state {
	case event.StateWorking:
		return "working"
	case event.StateNeedsInput:
		return "NEEDS YOU"
	case event.StateIdle:
		return "idle"
	}
	return "no session"
}

func humanAge(unix int64) string {
	if unix <= 0 {
		return "—"
	}
	d := time.Since(time.Unix(unix, 0))
	switch {
	case d < 0:
		return "0s"
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
	}
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
