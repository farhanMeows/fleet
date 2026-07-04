// Package notify sends user-facing alerts. On macOS it shells out to
// osascript (no extra dependencies). Alerts are debounced per project+kind
// and suppressed when the user is already looking at that project's tmux
// window.
package notify

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/farhanahmad/fleet/internal/tmuxdrv"
)

const debounceWindow = 20 * time.Second

type Notifier struct {
	stampDir string
}

func New(dir string) *Notifier {
	stampDir := filepath.Join(dir, "notify-stamps")
	os.MkdirAll(stampDir, 0o755)
	return &Notifier{stampDir: stampDir}
}

// PermissionNeeded alerts that a project's agent is waiting for approval.
func (n *Notifier) PermissionNeeded(project, tool, summary string) {
	if n.debounced(project, "perm") {
		return
	}
	body := tool
	if summary != "" {
		body = tool + ": " + summary
	}
	send("Claude needs approval — "+project, body, "Glass")
}

// TurnDone alerts that a project's agent finished a turn, unless the user is
// focused on that project's window already.
func (n *Notifier) TurnDone(project string) {
	if tmuxdrv.FocusedProject() == project {
		return
	}
	if n.debounced(project, "done") {
		return
	}
	send("Claude done — "+project, "agent is idle, ready for next task", "Pop")
}

// debounced returns true (and does not stamp) if an alert of this kind fired
// for the project within the debounce window.
func (n *Notifier) debounced(project, kind string) bool {
	stamp := filepath.Join(n.stampDir, sanitize(project)+"-"+kind)
	if fi, err := os.Stat(stamp); err == nil && time.Since(fi.ModTime()) < debounceWindow {
		return true
	}
	os.WriteFile(stamp, nil, 0o644)
	return false
}

func send(title, body, sound string) {
	script := fmt.Sprintf("display notification %q with title %q sound name %q", clip(body, 120), title, sound)
	exec.Command("osascript", "-e", script).Run()
}

func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '/' || r == '.' {
			return '_'
		}
		return r
	}, s)
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
