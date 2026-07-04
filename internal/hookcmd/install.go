package hookcmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// hookedEvents are the Claude Code hook events fleet subscribes to.
var hookedEvents = []string{"SessionStart", "PreToolUse", "PermissionRequest", "Stop", "SessionEnd"}

// Install wires `fleet hook <event>` entries into ~/.claude/settings.json,
// backing the file up first. Existing non-fleet hooks are preserved; existing
// fleet entries are replaced (so re-running after moving the binary is safe).
func Install() error {
	bin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve fleet binary path: %w", err)
	}
	bin, _ = filepath.EvalSymlinks(bin)

	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	settingsPath := filepath.Join(home, ".claude", "settings.json")

	settings := map[string]any{}
	raw, err := os.ReadFile(settingsPath)
	switch {
	case err == nil:
		if err := json.Unmarshal(raw, &settings); err != nil {
			return fmt.Errorf("parse %s: %w", settingsPath, err)
		}
		backup := fmt.Sprintf("%s.fleet-backup-%s", settingsPath, time.Now().Format("20060102-150405"))
		if err := os.WriteFile(backup, raw, 0o600); err != nil {
			return fmt.Errorf("write backup: %w", err)
		}
		fmt.Printf("backed up settings to %s\n", backup)
	case os.IsNotExist(err):
		// fresh settings file
	default:
		return err
	}

	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	for _, ev := range hookedEvents {
		entry := map[string]any{
			"hooks": []any{map[string]any{
				"type":    "command",
				"command": fmt.Sprintf("%s hook %s", bin, ev),
			}},
		}
		if ev == "PreToolUse" || ev == "PermissionRequest" {
			entry["matcher"] = "*"
		}
		existing, _ := hooks[ev].([]any)
		kept := existing[:0]
		for _, e := range existing {
			if !isFleetEntry(e) {
				kept = append(kept, e)
			}
		}
		entries := []any{}
		// The prod-data guard runs first on Bash calls; it is the only fleet
		// hook that may deny (exit 2).
		if ev == "PreToolUse" {
			entries = append(entries, map[string]any{
				"matcher": "Bash",
				"hooks": []any{map[string]any{
					"type":    "command",
					"command": bin + " guard",
				}},
			})
		}
		entries = append(entries, entry)
		hooks[ev] = append(kept, entries...)
	}
	settings["hooks"] = hooks

	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(settingsPath, append(out, '\n'), 0o644); err != nil {
		return err
	}
	fmt.Printf("installed fleet hooks (%s) into %s\n", bin, settingsPath)
	fmt.Println("note: hooks take effect in new claude sessions (or after /hooks reload)")
	return nil
}

// isFleetEntry reports whether a hook matcher-group contains only fleet commands.
func isFleetEntry(e any) bool {
	m, ok := e.(map[string]any)
	if !ok {
		return false
	}
	inner, _ := m["hooks"].([]any)
	if len(inner) == 0 {
		return false
	}
	for _, h := range inner {
		hm, ok := h.(map[string]any)
		if !ok {
			return false
		}
		cmd, _ := hm["command"].(string)
		if !isFleetHookCommand(cmd) {
			return false
		}
	}
	return true
}

func isFleetHookCommand(cmd string) bool {
	// Matches "<anything>/fleet hook <Event>" and "<anything>/fleet guard"
	// regardless of install location.
	if cmd == "" {
		return false
	}
	base := filepath.Base(firstField(cmd))
	return base == "fleet" && (containsWord(cmd, "hook") || containsWord(cmd, "guard"))
}

func firstField(s string) string {
	for i, r := range s {
		if r == ' ' {
			return s[:i]
		}
	}
	return s
}

func containsWord(s, w string) bool {
	fields := []string{}
	cur := ""
	for _, r := range s {
		if r == ' ' {
			if cur != "" {
				fields = append(fields, cur)
				cur = ""
			}
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		fields = append(fields, cur)
	}
	for _, f := range fields {
		if f == w {
			return true
		}
	}
	return false
}
