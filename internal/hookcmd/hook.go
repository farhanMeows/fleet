// Package hookcmd implements `fleet hook <event>` — the command Claude Code
// hooks invoke. Contract: read stdin once, never write to stdout (PreToolUse
// stdout can be interpreted as a permission decision), never block Claude,
// and always exit 0. Failures are logged to ~/.fleet/hook.log and events are
// spooled to disk when the daemon is unreachable.
package hookcmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/farhanahmad/fleet/internal/config"
	"github.com/farhanahmad/fleet/internal/event"
)

const (
	maxStdin    = 1 << 20 // 1 MiB
	postTimeout = 700 * time.Millisecond
)

// Run processes one hook invocation. It never returns an error to the caller;
// the process must exit 0 regardless.
func Run(eventName string, stdin io.Reader) {
	cfg, err := config.Load()
	if err != nil {
		return // nowhere to log; stay silent and fast
	}
	logf := func(format string, args ...any) {
		f, ferr := os.OpenFile(cfg.LogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if ferr != nil {
			return
		}
		defer f.Close()
		fmt.Fprintf(f, "%s [%s] %s\n", time.Now().Format(time.RFC3339), eventName, fmt.Sprintf(format, args...))
	}

	raw, err := io.ReadAll(io.LimitReader(stdin, maxStdin))
	if err != nil {
		logf("read stdin: %v", err)
		return
	}
	var payload event.HookPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		logf("parse payload: %v", err)
		return
	}
	if payload.SessionID == "" || payload.Cwd == "" {
		logf("payload missing session_id or cwd")
		return
	}
	ev := event.FromPayload(eventName, &payload, time.Now().Unix())

	body, err := json.Marshal(ev)
	if err != nil {
		logf("marshal event: %v", err)
		return
	}
	if err := post(cfg.BaseURL()+"/api/hook", body); err != nil {
		if serr := spool(cfg.SpoolDir, body); serr != nil {
			logf("daemon unreachable (%v) and spool failed: %v", err, serr)
		}
	}
}

func post(url string, body []byte) error {
	client := &http.Client{Timeout: postTimeout}
	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("daemon returned %d", resp.StatusCode)
	}
	return nil
}

// spool writes the event to disk atomically for the daemon to drain later.
func spool(dir string, body []byte) error {
	name := fmt.Sprintf("%d-%d.json", time.Now().UnixNano(), os.Getpid())
	tmp := filepath.Join(dir, "."+name+".tmp")
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, name))
}
