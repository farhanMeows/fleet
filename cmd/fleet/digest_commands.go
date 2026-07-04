package main

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

func digestCmd() *cobra.Command {
	var yesterday bool
	cmd := &cobra.Command{
		Use:   "digest",
		Short: "Daily standup: what every agent did, per project",
		RunE: func(_ *cobra.Command, _ []string) error {
			day := time.Now()
			if yesterday {
				day = day.AddDate(0, 0, -1)
			}
			c, err := newClient()
			if err != nil {
				return err
			}
			d, err := c.Digest(day.Format("2006-01-02"))
			if err != nil {
				return err
			}
			fmt.Printf("FLEET DIGEST — %s\n\n", d.Day)
			if len(d.Projects) == 0 {
				fmt.Println("no agent activity")
				return nil
			}
			fmt.Printf("  %-26s %8s %6s %6s %12s %12s\n", "PROJECT", "SESSIONS", "TURNS", "TOOLS", "TOK IN", "TOK OUT")
			for _, p := range d.Projects {
				fmt.Printf("  %-26s %8d %6d %6d %12s %12s\n",
					p.Project, p.Sessions, p.Turns, p.ToolEvents, humanTokens(p.InputTokens), humanTokens(p.OutputTokens))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&yesterday, "yesterday", false, "digest for yesterday instead of today")
	return cmd
}

func portsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ports <project> <ports>",
		Short: "Set the dev-server ports health-checked for a project (e.g. 3000,3001)",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			if err := c.SetPorts(args[0], args[1]); err != nil {
				return err
			}
			fmt.Printf("ports for %s set to %s\n", args[0], args[1])
			return nil
		},
	}
}

func humanTokens(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1_000)
	}
	return fmt.Sprintf("%d", n)
}
