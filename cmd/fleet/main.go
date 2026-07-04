// fleet — mission control for Claude Code agents.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/farhanahmad/fleet/internal/config"
	"github.com/farhanahmad/fleet/internal/hookcmd"
	"github.com/farhanahmad/fleet/internal/server"
	"github.com/farhanahmad/fleet/internal/store"
)

func main() {
	root := &cobra.Command{
		Use:           "fleet",
		Short:         "Mission control for Claude Code agents across all your projects",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.AddCommand(
		hookCmd(),
		daemonCmd(),
		installCmd(),
		addCmd(),
		removeCmd(),
		listCmd(),
		upCmd(),
		statusCmd(),
		dispatchCmd(),
		queueCmd(),
		playbookCmd(),
		broadcastCmd(),
	)

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "fleet:", err)
		os.Exit(1)
	}
}

// hookCmd is invoked by Claude Code hooks. It must never fail, never print
// to stdout, and return fast — see internal/hookcmd.
func hookCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "hook <event>",
		Short:  "Ingest a Claude Code hook event from stdin (called by hooks, not humans)",
		Args:   cobra.ExactArgs(1),
		Hidden: true,
		Run: func(_ *cobra.Command, args []string) {
			hookcmd.Run(args[0], os.Stdin)
		},
	}
}

func daemonCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "daemon",
		Short: "Run the fleet daemon (API + dashboard) in the foreground",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			st, err := store.Open(cfg.DBPath)
			if err != nil {
				return err
			}
			defer st.Close()
			srv, err := server.New(cfg, st)
			if err != nil {
				return err
			}
			return srv.Run()
		},
	}
}

func installCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "install",
		Short: "Install fleet's hooks into ~/.claude/settings.json (backs up first)",
		RunE: func(_ *cobra.Command, _ []string) error {
			return hookcmd.Install()
		},
	}
}
