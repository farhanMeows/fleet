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
