// Package client is the CLI-side API client for the fleet daemon,
// including transparent daemon auto-start.
package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"time"

	"strings"

	"github.com/farhanahmad/fleet/internal/config"
	"github.com/farhanahmad/fleet/internal/queue"
	"github.com/farhanahmad/fleet/internal/store"
)

type Client struct {
	cfg  *config.Config
	http *http.Client
}

func New(cfg *config.Config) *Client {
	return &Client{cfg: cfg, http: &http.Client{Timeout: 5 * time.Second}}
}

func (c *Client) Healthy() bool {
	resp, err := c.http.Get(c.cfg.BaseURL() + "/healthz")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return resp.StatusCode == http.StatusOK
}

// EnsureDaemon starts `fleet daemon` detached if it is not already running.
func (c *Client) EnsureDaemon() error {
	if c.Healthy() {
		return nil
	}
	bin, err := os.Executable()
	if err != nil {
		return err
	}
	logf, err := os.OpenFile(c.cfg.Dir+"/daemon.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer logf.Close()
	cmd := exec.Command(bin, "daemon")
	cmd.Stdout = logf
	cmd.Stderr = logf
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}
	if err := cmd.Process.Release(); err != nil {
		return err
	}
	for i := 0; i < 30; i++ {
		if c.Healthy() {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("daemon did not become healthy on %s (see %s/daemon.log)", c.cfg.BaseURL(), c.cfg.Dir)
}

func (c *Client) get(path string, out any) error {
	resp, err := c.http.Get(c.cfg.BaseURL() + path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s: %s", resp.Status, bytes.TrimSpace(body))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) send(method, path string, in any) error {
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, c.cfg.BaseURL()+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s: %s", resp.Status, bytes.TrimSpace(raw))
	}
	io.Copy(io.Discard, resp.Body)
	return nil
}

func (c *Client) Sessions() ([]store.Session, error) {
	var out struct {
		Sessions []store.Session `json:"sessions"`
	}
	err := c.get("/api/sessions", &out)
	return out.Sessions, err
}

func (c *Client) Projects() ([]store.Project, error) {
	var out struct {
		Projects []store.Project `json:"projects"`
	}
	err := c.get("/api/projects", &out)
	return out.Projects, err
}

func (c *Client) AddProject(name, path string) error {
	return c.send(http.MethodPost, "/api/projects", map[string]string{"name": name, "path": path})
}

func (c *Client) RemoveProject(name string) error {
	return c.send(http.MethodDelete, "/api/projects/"+name, nil)
}

func (c *Client) Dispatch(project, prompt string, force bool) error {
	return c.send(http.MethodPost, "/api/dispatch",
		map[string]any{"project": project, "prompt": prompt, "force": force})
}

func (c *Client) QueueAdd(project, prompt string) (*queue.Item, error) {
	raw, err := json.Marshal(map[string]string{"project": project, "prompt": prompt})
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Post(c.cfg.BaseURL()+"/api/queue", "application/json", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%s: %s", resp.Status, bytes.TrimSpace(body))
	}
	var item queue.Item
	if err := json.NewDecoder(resp.Body).Decode(&item); err != nil {
		return nil, err
	}
	return &item, nil
}

func (c *Client) QueueList(project string) ([]queue.Item, error) {
	path := "/api/queue"
	if project != "" {
		path += "?project=" + url.QueryEscape(project)
	}
	var out struct {
		Items []queue.Item `json:"items"`
	}
	err := c.get(path, &out)
	return out.Items, err
}

func (c *Client) QueueCancel(id string) error {
	return c.send(http.MethodDelete, "/api/queue/"+id, nil)
}

func (c *Client) Playbooks() ([]queue.Playbook, error) {
	var out struct {
		Playbooks []queue.Playbook `json:"playbooks"`
	}
	err := c.get("/api/playbooks", &out)
	return out.Playbooks, err
}

func (c *Client) PlaybookSave(name, prompt string) error {
	return c.send(http.MethodPost, "/api/playbooks", map[string]string{"name": name, "prompt": prompt})
}

func (c *Client) PlaybookDelete(name string) error {
	return c.send(http.MethodDelete, "/api/playbooks/"+name, nil)
}

func (c *Client) Events(limit int) ([]store.EventRow, error) {
	var out struct {
		Events []store.EventRow `json:"events"`
	}
	err := c.get(fmt.Sprintf("/api/events?limit=%d", limit), &out)
	return out.Events, err
}

// Costs returns daily token usage rows for the last N days.
func (c *Client) Costs(days int) ([]store.UsageRow, error) {
	var out struct {
		Usage []store.UsageRow `json:"usage"`
	}
	err := c.get(fmt.Sprintf("/api/costs?days=%d", days), &out)
	return out.Usage, err
}

// ClaudeWindow is the estimated current 5-hour usage block.
type ClaudeWindow struct {
	Active       bool  `json:"active"`
	WindowStart  int64 `json:"window_start"`
	ResetsAt     int64 `json:"resets_at"`
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
	Turns        int64 `json:"turns"`
	Budget       int64 `json:"budget"`
}

func (c *Client) ClaudeWindow() (*ClaudeWindow, error) {
	var out ClaudeWindow
	if err := c.get("/api/claude-window", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Approve answers a project's pending permission prompt remotely.
// Returns what was approved/denied (tool + summary) for relaying to the user.
func (c *Client) Approve(project string, deny bool) (tool, summary string, err error) {
	raw, err := json.Marshal(map[string]any{"project": project, "deny": deny})
	if err != nil {
		return "", "", err
	}
	resp, err := c.http.Post(c.cfg.BaseURL()+"/api/approve", "application/json", bytes.NewReader(raw))
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return "", "", fmt.Errorf("%s", bytes.TrimSpace(body))
	}
	var out struct{ Tool, Summary string }
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", "", err
	}
	return out.Tool, out.Summary, nil
}

type ResultRow struct {
	Project   string `json:"project"`
	Snippet   string `json:"snippet"`
	UpdatedAt int64  `json:"updated_at"`
}

// Results returns each project's most recent agent reply, newest first.
func (c *Client) Results() ([]ResultRow, error) {
	var out struct {
		Results []ResultRow `json:"results"`
	}
	err := c.get("/api/results", &out)
	return out.Results, err
}

type HealthRow struct {
	Project   string `json:"project"`
	GitBranch string `json:"git_branch"`
	GitDirty  int    `json:"git_dirty"`
	Pm2       []struct {
		Name   string `json:"name"`
		Status string `json:"status"`
	} `json:"pm2"`
	Ports []struct {
		Port int  `json:"port"`
		Open bool `json:"open"`
	} `json:"ports"`
}

func (c *Client) Health() (map[string]HealthRow, error) {
	var out struct {
		Projects []HealthRow `json:"projects"`
	}
	if err := c.get("/api/health", &out); err != nil {
		return nil, err
	}
	m := map[string]HealthRow{}
	for _, h := range out.Projects {
		m[h.Project] = h
	}
	return m, nil
}

type DigestRow struct {
	Project      string `json:"project"`
	Sessions     int64  `json:"sessions"`
	Turns        int64  `json:"turns"`
	ToolEvents   int64  `json:"tool_events"`
	InputTokens  int64  `json:"input_tokens"`
	OutputTokens int64  `json:"output_tokens"`
}

type Digest struct {
	Day      string      `json:"day"`
	Projects []DigestRow `json:"projects"`
}

func (c *Client) Digest(day string) (*Digest, error) {
	var out Digest
	if err := c.get("/api/digest?day="+url.QueryEscape(day), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// LastReply returns the trailing assistant messages (most recent last) from
// a project's most recently active session, skipping tool-call lines.
func (c *Client) LastReply(project string, n int) ([]string, error) {
	var sessOut struct {
		Sessions []store.Session `json:"sessions"`
	}
	if err := c.get("/api/sessions?all=1", &sessOut); err != nil {
		return nil, err
	}
	var latest *store.Session
	for i := range sessOut.Sessions {
		s := &sessOut.Sessions[i]
		if s.Project == project && s.TranscriptPath != "" && (latest == nil || s.UpdatedAt > latest.UpdatedAt) {
			latest = s
		}
	}
	if latest == nil {
		return nil, fmt.Errorf("no session with a transcript found for project %q", project)
	}
	var tOut struct {
		Entries []struct {
			Role string `json:"role"`
			Text string `json:"text"`
		} `json:"entries"`
	}
	if err := c.get("/api/transcript/"+latest.SessionID, &tOut); err != nil {
		return nil, err
	}
	var replies []string
	for _, e := range tOut.Entries {
		if e.Role == "assistant" && !strings.HasPrefix(e.Text, "→ ") {
			replies = append(replies, e.Text)
		}
	}
	if len(replies) == 0 {
		return nil, fmt.Errorf("no assistant replies in the transcript yet")
	}
	if n > 0 && len(replies) > n {
		replies = replies[len(replies)-n:]
	}
	return replies, nil
}

func (c *Client) SetPorts(project, ports string) error {
	return c.send(http.MethodPut, "/api/projects/"+project+"/ports", map[string]string{"ports": ports})
}

func (c *Client) Broadcast(prompt, playbook string, projects []string, all bool) ([]string, error) {
	raw, err := json.Marshal(map[string]any{
		"prompt": prompt, "playbook": playbook, "projects": projects, "all": all,
	})
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Post(c.cfg.BaseURL()+"/api/broadcast", "application/json", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%s: %s", resp.Status, bytes.TrimSpace(body))
	}
	var out struct {
		Projects []string `json:"projects"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Projects, nil
}
