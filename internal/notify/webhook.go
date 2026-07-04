package notify

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// WebhooksFile lists outbox URLs, one per line (# comments allowed).
// Every alert is POSTed to each URL as JSON — consumable by hermes-agent,
// ntfy, a Telegram bot bridge, or anything that accepts a webhook.
const WebhooksFile = "webhooks.txt"

// Alert is the webhook payload. Schema-stable: additive changes only.
type Alert struct {
	Kind    string `json:"kind"` // permission_needed | turn_done
	Project string `json:"project"`
	Tool    string `json:"tool,omitempty"`
	Summary string `json:"summary,omitempty"`
	Ts      int64  `json:"ts"`
}

func LoadWebhooks(fleetDir string) []string {
	f, err := os.Open(filepath.Join(fleetDir, WebhooksFile))
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			out = append(out, line)
		}
	}
	return out
}

// SendWebhooks fires the alert at every configured URL, best-effort.
// Telegram Bot API URLs (api.telegram.org/bot<token>/sendMessage?chat_id=N)
// get a human-readable text message; everything else gets the JSON payload.
func SendWebhooks(fleetDir string, a Alert) {
	urls := LoadWebhooks(fleetDir)
	if len(urls) == 0 {
		return
	}
	body, err := json.Marshal(a)
	if err != nil {
		return
	}
	client := &http.Client{Timeout: 5 * time.Second}
	for _, u := range urls {
		go func(target string) {
			var resp *http.Response
			var err error
			if strings.Contains(target, "api.telegram.org") {
				resp, err = client.PostForm(target, url.Values{"text": {a.Text()}})
			} else {
				resp, err = client.Post(target, "application/json", bytes.NewReader(body))
			}
			if err == nil {
				resp.Body.Close()
			}
		}(u)
	}
}

// Text renders the alert for humans (Telegram, ntfy, etc.).
func (a Alert) Text() string {
	switch a.Kind {
	case "permission_needed":
		msg := "⚠ " + a.Project + " needs your approval"
		if a.Tool != "" {
			msg += "\n" + a.Tool
			if a.Summary != "" {
				msg += ": " + a.Summary
			}
		}
		return msg
	case "turn_done":
		return "✓ " + a.Project + " finished a task"
	}
	return a.Kind + ": " + a.Project
}
