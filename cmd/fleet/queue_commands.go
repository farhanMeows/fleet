package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func queueCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "queue",
		Short: "Queue prompts for a project's agent (auto-dispatched when it goes idle)",
	}

	add := &cobra.Command{
		Use:   "add <project> <prompt>",
		Short: "Enqueue a prompt for a project",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			item, err := c.QueueAdd(args[0], strings.Join(args[1:], " "))
			if err != nil {
				return err
			}
			fmt.Printf("queued #%d for %s\n", item.ID, item.Project)
			return nil
		},
	}

	list := &cobra.Command{
		Use:   "list [project]",
		Short: "Show queued prompts",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			project := ""
			if len(args) == 1 {
				project = args[0]
			}
			c, err := newClient()
			if err != nil {
				return err
			}
			items, err := c.QueueList(project)
			if err != nil {
				return err
			}
			if len(items) == 0 {
				fmt.Println("queue is empty")
				return nil
			}
			for _, it := range items {
				fmt.Printf("#%-4d %-24s %s  (queued %s)\n",
					it.ID, it.Project, clip(it.Prompt, 60), humanAge(it.CreatedAt))
			}
			return nil
		},
	}

	cancel := &cobra.Command{
		Use:   "cancel <id>",
		Short: "Cancel a queued prompt",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			if err := c.QueueCancel(args[0]); err != nil {
				return err
			}
			fmt.Println("cancelled")
			return nil
		},
	}

	cmd.AddCommand(add, list, cancel)
	return cmd
}

func playbookCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "playbook",
		Short: "Reusable prompts ({{project}} is substituted at run time)",
	}

	save := &cobra.Command{
		Use:   "save <name> <prompt>",
		Short: "Create or update a playbook",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			if err := c.PlaybookSave(args[0], strings.Join(args[1:], " ")); err != nil {
				return err
			}
			fmt.Printf("saved playbook %s\n", args[0])
			return nil
		},
	}

	list := &cobra.Command{
		Use:   "list",
		Short: "List playbooks",
		RunE: func(_ *cobra.Command, _ []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			books, err := c.Playbooks()
			if err != nil {
				return err
			}
			if len(books) == 0 {
				fmt.Println("no playbooks — try: fleet playbook save run-tests \"run the test suite and fix any failures\"")
				return nil
			}
			for _, b := range books {
				fmt.Printf("%-20s %s\n", b.Name, clip(b.Prompt, 70))
			}
			return nil
		},
	}

	del := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a playbook",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			if err := c.PlaybookDelete(args[0]); err != nil {
				return err
			}
			fmt.Println("deleted")
			return nil
		},
	}

	run := &cobra.Command{
		Use:   "run <name> <project> [project...]",
		Short: "Queue a playbook on one or more projects",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			queued, err := c.Broadcast("", args[0], args[1:], false)
			if err != nil {
				return err
			}
			fmt.Printf("queued playbook %s on: %s\n", args[0], strings.Join(queued, ", "))
			return nil
		},
	}

	cmd.AddCommand(save, list, del, run)
	return cmd
}

func broadcastCmd() *cobra.Command {
	var projects []string
	var all bool
	cmd := &cobra.Command{
		Use:   "broadcast <prompt>",
		Short: "Queue one prompt across many projects",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if !all && len(projects) == 0 {
				return fmt.Errorf("pass --projects a,b,c or --all")
			}
			c, err := newClient()
			if err != nil {
				return err
			}
			queued, err := c.Broadcast(strings.Join(args, " "), "", projects, all)
			if err != nil {
				return err
			}
			fmt.Printf("queued on: %s\n", strings.Join(queued, ", "))
			return nil
		},
	}
	cmd.Flags().StringSliceVar(&projects, "projects", nil, "comma-separated project names")
	cmd.Flags().BoolVar(&all, "all", false, "all registered projects")
	return cmd
}

// humanAge for unix seconds in the past (shared with commands.go via same package).
var _ = time.Now
