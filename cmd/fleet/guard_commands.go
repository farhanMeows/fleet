package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/farhanahmad/fleet/internal/config"
	"github.com/farhanahmad/fleet/internal/event"
	"github.com/farhanahmad/fleet/internal/guard"
)

func guardCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "guard",
		Short: "Prod-data guardrail: block destructive commands that reference prod",
		// Bare `fleet guard` is the PreToolUse hook entrypoint (matcher: Bash).
		// Exit 2 + stderr = deny with reason; anything unexpected exits 0
		// (fail open — the CLAUDE.md rule is the second layer).
		Run: func(_ *cobra.Command, _ []string) {
			runGuardHook(os.Stdin)
		},
	}

	add := &cobra.Command{
		Use:   "add <pattern>",
		Short: "Add a prod pattern (substring or regex), e.g. a prod DB host",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if err := guard.Add(cfg.Dir, args[0]); err != nil {
				return err
			}
			fmt.Printf("added guard pattern: %s\n", args[0])
			return nil
		},
	}

	list := &cobra.Command{
		Use:   "list",
		Short: "List prod patterns",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			patterns, err := guard.Load(cfg.Dir)
			if err != nil {
				return err
			}
			if len(patterns) == 0 {
				fmt.Println("no guard patterns — try: fleet guard add my-prod-host.example.com")
				return nil
			}
			for _, p := range patterns {
				fmt.Println(p)
			}
			return nil
		},
	}

	remove := &cobra.Command{
		Use:   "remove <pattern>",
		Short: "Remove a prod pattern",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if err := guard.Remove(cfg.Dir, args[0]); err != nil {
				return err
			}
			fmt.Println("removed")
			return nil
		},
	}

	check := &cobra.Command{
		Use:   "check <command>",
		Short: "Dry-run a command against the guard rules",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			patterns, err := guard.Load(cfg.Dir)
			if err != nil {
				return err
			}
			cmd := args[0]
			for _, a := range args[1:] {
				cmd += " " + a
			}
			v := guard.Check(cmd, patterns)
			if v.Blocked {
				fmt.Printf("BLOCKED — prod pattern %q + destructive %q\n", v.Pattern, v.Verb)
			} else {
				fmt.Println("allowed")
			}
			return nil
		},
	}

	cmd.AddCommand(add, list, remove, check)
	return cmd
}

// runGuardHook implements the PreToolUse hook contract. It must be fast and
// must not print to stdout except a decision; we use exit codes only.
func runGuardHook(stdin io.Reader) {
	cfg, err := config.Load()
	if err != nil {
		return // fail open
	}
	patterns, err := guard.Load(cfg.Dir)
	if err != nil || len(patterns) == 0 {
		return
	}
	raw, err := io.ReadAll(io.LimitReader(stdin, 1<<20))
	if err != nil {
		return
	}
	var payload event.HookPayload
	if json.Unmarshal(raw, &payload) != nil || payload.ToolName != "Bash" {
		return
	}
	var input struct {
		Command string `json:"command"`
	}
	if json.Unmarshal(payload.ToolInput, &input) != nil || input.Command == "" {
		return
	}
	if v := guard.Check(input.Command, patterns); v.Blocked {
		fmt.Fprintf(os.Stderr,
			"fleet guard: BLOCKED — this command combines a production reference (%q) with a destructive operation (%q). "+
				"Rule: never update or delete production data; do the work against dev and roll out to prod only after explicit human confirmation.",
			v.Pattern, v.Verb)
		os.Exit(2)
	}
}
