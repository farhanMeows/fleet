// Package health probes per-project git state, pm2 processes, and dev-server
// ports so the dashboard can show "agent done but server down" at a glance.
package health

import (
	"encoding/json"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/farhanahmad/fleet/internal/store"
)

type PortStatus struct {
	Port int  `json:"port"`
	Open bool `json:"open"`
}

type Pm2App struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

type ProjectHealth struct {
	Project   string       `json:"project"`
	GitBranch string       `json:"git_branch,omitempty"`
	GitDirty  int          `json:"git_dirty"` // changed-file count
	Pm2       []Pm2App     `json:"pm2,omitempty"`
	Ports     []PortStatus `json:"ports,omitempty"`
	CheckedAt int64        `json:"checked_at"`
}

type Prober struct {
	store *store.Store

	mu      sync.RWMutex
	results map[string]ProjectHealth
}

func NewProber(st *store.Store) *Prober {
	return &Prober{store: st, results: map[string]ProjectHealth{}}
}

// Run probes on an interval until the process exits.
func (p *Prober) Run(interval time.Duration) {
	p.probeAll()
	for range time.Tick(interval) {
		p.probeAll()
	}
}

func (p *Prober) Results() []ProjectHealth {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]ProjectHealth, 0, len(p.results))
	for _, h := range p.results {
		out = append(out, h)
	}
	return out
}

func (p *Prober) probeAll() {
	projects, err := p.store.ListProjects()
	if err != nil {
		return
	}
	pm2ByCwd := probePm2()
	fresh := map[string]ProjectHealth{}
	for _, proj := range projects {
		h := ProjectHealth{Project: proj.Name, CheckedAt: time.Now().Unix()}
		h.GitBranch, h.GitDirty = probeGit(proj.Path)
		for cwd, apps := range pm2ByCwd {
			if cwd == proj.Path || strings.HasPrefix(cwd, proj.Path+"/") {
				h.Pm2 = append(h.Pm2, apps...)
			}
		}
		for _, port := range parsePorts(proj.Ports) {
			h.Ports = append(h.Ports, PortStatus{Port: port, Open: portOpen(port)})
		}
		fresh[proj.Name] = h
	}
	p.mu.Lock()
	p.results = fresh
	p.mu.Unlock()
}

func probeGit(path string) (branch string, dirty int) {
	out, err := exec.Command("git", "-C", path, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return "", 0 // not a git repo
	}
	branch = strings.TrimSpace(string(out))
	status, err := exec.Command("git", "-C", path, "status", "--porcelain").Output()
	if err != nil {
		return branch, 0
	}
	for _, line := range strings.Split(string(status), "\n") {
		if strings.TrimSpace(line) != "" {
			dirty++
		}
	}
	return branch, dirty
}

// probePm2 returns pm2 apps grouped by their working directory. Missing pm2
// (or no daemon) yields an empty map.
func probePm2() map[string][]Pm2App {
	out, err := exec.Command("pm2", "jlist").Output()
	if err != nil {
		return nil
	}
	var apps []struct {
		Name   string `json:"name"`
		Pm2Env struct {
			Status string `json:"status"`
			Cwd    string `json:"pm_cwd"`
		} `json:"pm2_env"`
	}
	if json.Unmarshal(out, &apps) != nil {
		return nil
	}
	byCwd := map[string][]Pm2App{}
	for _, a := range apps {
		byCwd[a.Pm2Env.Cwd] = append(byCwd[a.Pm2Env.Cwd], Pm2App{Name: a.Name, Status: a.Pm2Env.Status})
	}
	return byCwd
}

func portOpen(port int) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 500*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func parsePorts(s string) []int {
	var out []int
	for _, part := range strings.Split(s, ",") {
		var p int
		if _, err := fmt.Sscanf(strings.TrimSpace(part), "%d", &p); err == nil && p > 0 {
			out = append(out, p)
		}
	}
	return out
}
