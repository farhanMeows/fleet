// Package tmuxdrv drives the tmux mission-control session: one window per
// registered project running claude, tagged with the @fleet_project window
// option (window *names* carry live state icons, so lookups use the tag).
package tmuxdrv

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/farhanahmad/fleet/internal/event"
	"github.com/farhanahmad/fleet/internal/store"
)

const SessionName = "fleet"

// Icons keyed by project state; empty state means no live session.
var icons = map[string]string{
	event.StateWorking:    "●",
	event.StateNeedsInput: "⚠",
	event.StateIdle:       "✓",
	"":                    "○",
}

func tmux(args ...string) (string, error) {
	out, err := exec.Command("tmux", args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func HasSession() bool {
	_, err := tmux("has-session", "-t", SessionName)
	return err == nil
}

// Up creates (or completes) the fleet session: window 0 runs the status
// watcher, then one window per project. Existing project windows are kept.
func Up(projects []store.Project, fleetBin string) error {
	if !HasSession() {
		// Window 0: dashboard. -d: don't attach yet.
		if _, err := tmux("new-session", "-d", "-s", SessionName, "-n", "dash",
			fleetBin+" status --watch"); err != nil {
			return fmt.Errorf("create session: %w", err)
		}
		tmux("set-option", "-t", SessionName, "-g", "allow-rename", "off")
		// Hotkeys: prefix+g = dashboard, prefix+j = window picker.
		tmux("bind-key", "g", "select-window", "-t", SessionName+":0")
		tmux("bind-key", "j", "choose-tree", "-Zw")
	}

	existing, err := windowsByProject()
	if err != nil {
		return err
	}
	for _, p := range projects {
		if _, ok := existing[p.Name]; ok {
			continue
		}
		// New window with the user's shell, then launch claude via send-keys
		// so the window survives claude exiting.
		if _, err := tmux("new-window", "-t", SessionName, "-d", "-n", icons[""]+" "+p.Name, "-c", p.Path); err != nil {
			return fmt.Errorf("window for %s: %w", p.Name, err)
		}
		target := SessionName + ":$" // last created — resolve explicitly instead
		idx, err := findWindowByName(icons[""] + " " + p.Name)
		if err != nil {
			return err
		}
		target = fmt.Sprintf("%s:%s", SessionName, idx)
		tmux("set-option", "-w", "-t", target, "@fleet_project", p.Name)
		tmux("set-option", "-w", "-t", target, "automatic-rename", "off")
		tmux("send-keys", "-t", target, "claude", "Enter")
	}
	return nil
}

// Attach execs into the fleet session (replacing the current process) or
// switches the current tmux client if already inside tmux.
func Attach() error {
	if os.Getenv("TMUX") != "" {
		_, err := tmux("switch-client", "-t", SessionName)
		return err
	}
	path, err := exec.LookPath("tmux")
	if err != nil {
		return err
	}
	return syscallExec(path, []string{"tmux", "attach-session", "-t", SessionName})
}

// SetIcon renames a project's window to reflect its current state.
// Safe to call from the daemon: no-ops when tmux/session/window is absent.
func SetIcon(project, state string) {
	if !HasSession() {
		return
	}
	windows, err := windowsByProject()
	if err != nil {
		return
	}
	idx, ok := windows[project]
	if !ok {
		return
	}
	icon, ok := icons[state]
	if !ok {
		icon = icons[""]
	}
	tmux("rename-window", "-t", fmt.Sprintf("%s:%s", SessionName, idx), icon+" "+project)
}

// FocusedProject returns the @fleet_project of the fleet session's active
// window when a client is attached to it, else "".
func FocusedProject() string {
	out, err := tmux("display-message", "-p", "-t", SessionName, "#{session_attached}\t#{@fleet_project}")
	if err != nil {
		return ""
	}
	parts := strings.SplitN(out, "\t", 2)
	if len(parts) != 2 || parts[0] == "0" || parts[0] == "" {
		return ""
	}
	return parts[1]
}

// Dispatch pastes a prompt into a project's claude window and submits it.
// Bracketed paste (-p) delivers multi-line prompts as one paste; Enter is
// sent separately after a short delay — a same-burst Enter is swallowed by
// claude's input box.
func Dispatch(project, prompt string) error {
	windows, err := windowsByProject()
	if err != nil {
		return fmt.Errorf("fleet tmux session not running — start it with `fleet up`")
	}
	idx, ok := windows[project]
	if !ok {
		return fmt.Errorf("no tmux window for project %q — run `fleet up` to create it", project)
	}
	target := fmt.Sprintf("%s:%s", SessionName, idx)

	load := exec.Command("tmux", "load-buffer", "-b", "fleet-dispatch", "-")
	load.Stdin = strings.NewReader(prompt)
	if out, err := load.CombinedOutput(); err != nil {
		return fmt.Errorf("load-buffer: %s", strings.TrimSpace(string(out)))
	}
	if _, err := tmux("paste-buffer", "-p", "-d", "-b", "fleet-dispatch", "-t", target); err != nil {
		return fmt.Errorf("paste-buffer: %w", err)
	}
	time.Sleep(400 * time.Millisecond)
	if _, err := tmux("send-keys", "-t", target, "Enter"); err != nil {
		return fmt.Errorf("submit: %w", err)
	}
	return nil
}

// windowsByProject maps @fleet_project tag -> window index.
func windowsByProject() (map[string]string, error) {
	out, err := tmux("list-windows", "-t", SessionName, "-F", "#{window_index}\t#{@fleet_project}")
	if err != nil {
		return nil, err
	}
	m := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) == 2 && parts[1] != "" {
			m[parts[1]] = parts[0]
		}
	}
	return m, nil
}

func findWindowByName(name string) (string, error) {
	out, err := tmux("list-windows", "-t", SessionName, "-F", "#{window_index}\t#{window_name}")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) == 2 && parts[1] == name {
			return parts[0], nil
		}
	}
	return "", fmt.Errorf("window %q not found", name)
}
