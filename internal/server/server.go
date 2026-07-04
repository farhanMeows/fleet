// Package server implements the fleet daemon: event ingestion, REST API,
// and SSE stream for live dashboard updates.
package server

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/farhanahmad/fleet/internal/config"
	"github.com/farhanahmad/fleet/internal/event"
	"github.com/farhanahmad/fleet/internal/health"
	"github.com/farhanahmad/fleet/internal/notify"
	"github.com/farhanahmad/fleet/internal/queue"
	"github.com/farhanahmad/fleet/internal/store"
	"github.com/farhanahmad/fleet/internal/tmuxdrv"
	"github.com/farhanahmad/fleet/internal/transcript"
	webdist "github.com/farhanahmad/fleet/web"
)

type Server struct {
	cfg      *config.Config
	store    *store.Store
	hub      *hub
	notifier *notify.Notifier
	queue    *queue.Queue
	prober   *health.Prober

	dispatchMu sync.Mutex // serializes queue dispatches across projects
	// inFlight marks projects with a queue-dispatched prompt whose turn has
	// not ended yet. Session state alone can't tell (a turn with no tool use
	// never leaves "idle"), so runQueue waits for the next Stop to clear it.
	inFlight map[string]time.Time
}

// inFlightExpiry unsticks a project if its Stop never arrives (killed session).
const inFlightExpiry = 15 * time.Minute

// minTurnForDoneAlert filters "done" notifications to turns that were real
// tasks, not quick chat exchanges.
const minTurnForDoneAlert = 30 * time.Second

func New(cfg *config.Config, st *store.Store) (*Server, error) {
	q, err := queue.New(st.DB())
	if err != nil {
		return nil, err
	}
	return &Server{
		cfg: cfg, store: st, hub: newHub(), notifier: notify.New(cfg.Dir),
		queue: q, prober: health.NewProber(st), inFlight: map[string]time.Time{},
	}, nil
}

// Run drains the spool, then serves until the process exits.
func (s *Server) Run() error {
	s.drainSpool()
	go s.watchSpool()
	go s.prober.Run(30 * time.Second)
	go s.reconcile()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/hook", s.handleHook)
	mux.HandleFunc("GET /api/sessions", s.handleSessions)
	mux.HandleFunc("GET /api/events", s.handleEvents)
	mux.HandleFunc("GET /api/projects", s.handleProjects)
	mux.HandleFunc("POST /api/projects", s.handleAddProject)
	mux.HandleFunc("DELETE /api/projects/{name}", s.handleRemoveProject)
	mux.HandleFunc("POST /api/dispatch", s.handleDispatch)
	mux.HandleFunc("GET /api/inbox", s.handleInbox)
	mux.HandleFunc("GET /api/transcript/{session_id}", s.handleTranscript)
	mux.HandleFunc("GET /api/queue", s.handleQueueList)
	mux.HandleFunc("POST /api/queue", s.handleQueueAdd)
	mux.HandleFunc("DELETE /api/queue/{id}", s.handleQueueCancel)
	mux.HandleFunc("GET /api/playbooks", s.handlePlaybookList)
	mux.HandleFunc("POST /api/playbooks", s.handlePlaybookSave)
	mux.HandleFunc("DELETE /api/playbooks/{name}", s.handlePlaybookDelete)
	mux.HandleFunc("POST /api/broadcast", s.handleBroadcast)
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/costs", s.handleCosts)
	mux.HandleFunc("GET /api/digest", s.handleDigest)
	mux.HandleFunc("PUT /api/projects/{name}/ports", s.handleSetPorts)
	if webFS, err := webdist.FS(); err == nil {
		mux.Handle("GET /", spaHandler(webFS))
	}
	mux.HandleFunc("GET /api/stream", s.hub.serveSSE)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, "ok")
	})

	addr := fmt.Sprintf("%s:%d", s.cfg.Bind, s.cfg.Port)
	log.Printf("fleet daemon listening on http://%s", addr)
	return http.ListenAndServe(addr, s.auth(mux))
}

// auth gates non-loopback requests behind the API token (~/.fleet/token).
// Loopback callers (hooks, local CLI, local browser) pass untouched, so the
// daemon stays zero-config until it is deliberately exposed (e.g. Tailscale
// via FLEET_BIND=0.0.0.0 for phone access through hermes-agent).
func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err == nil {
			if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
				next.ServeHTTP(w, r)
				return
			}
		}
		if s.cfg.Token == "" {
			http.Error(w, "remote access disabled: create ~/.fleet/token first", http.StatusForbidden)
			return
		}
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if got == "" {
			got = r.URL.Query().Get("token")
		}
		if subtle.ConstantTimeCompare([]byte(got), []byte(s.cfg.Token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleHook(w http.ResponseWriter, r *http.Request) {
	var ev event.Event
	if err := json.NewDecoder(r.Body).Decode(&ev); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	sess, err := s.apply(&ev)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, sess)
}

func (s *Server) apply(ev *event.Event) (*store.Session, error) {
	sess, err := s.store.ApplyEvent(ev)
	if err != nil {
		return nil, err
	}
	s.hub.broadcast("session", sess)
	// Side effects (tmux icon, notifications) run off the ingestion path.
	go s.react(ev, sess)
	return sess, nil
}

func (s *Server) react(ev *event.Event, sess *store.Session) {
	if state, err := s.store.ProjectState(sess.Project); err == nil {
		tmuxdrv.SetIcon(sess.Project, state)
	}
	switch ev.Event {
	case event.PermissionRequest:
		s.notifier.PermissionNeeded(sess.Project, ev.ToolName, ev.Summary)
		notify.SendWebhooks(s.cfg.Dir, notify.Alert{
			Kind: "permission_needed", Project: sess.Project,
			Tool: ev.ToolName, Summary: ev.Summary, Ts: ev.ReceivedAt,
		})
	case event.Stop:
		lastID := s.store.LastEventID(sess.SessionID)
		started := s.store.TurnStartedAt(sess.SessionID, lastID)
		if started > 0 && time.Duration(ev.ReceivedAt-started)*time.Second >= minTurnForDoneAlert {
			s.notifier.TurnDone(sess.Project)
			notify.SendWebhooks(s.cfg.Dir, notify.Alert{
				Kind: "turn_done", Project: sess.Project, Ts: ev.ReceivedAt,
				Summary: lastReplySnippet(sess.TranscriptPath),
			})
		}
		s.collectUsage(sess)
		s.dispatchMu.Lock()
		delete(s.inFlight, sess.Project)
		s.dispatchMu.Unlock()
		s.runQueue(sess.Project)
	}
}

// collectUsage folds the turn's token usage into the project's daily bucket.
func (s *Server) collectUsage(sess *store.Session) {
	if sess.TranscriptPath == "" {
		return
	}
	offset := s.store.UsageOffset(sess.SessionID)
	u, newOffset, err := transcript.TailUsage(sess.TranscriptPath, offset)
	if err != nil || newOffset == offset {
		return
	}
	day := time.Now().Format("2006-01-02")
	if err := s.store.AddUsage(sess.Project, day, u.InputTokens, u.OutputTokens, u.CacheRead, u.CacheCreate, u.Turns); err != nil {
		log.Printf("usage %s: %v", sess.Project, err)
		return
	}
	s.store.SetUsageOffset(sess.SessionID, newOffset)
}

// lastReplySnippet extracts the agent's final message of the turn so
// completion alerts carry the answer, not just "finished".
func lastReplySnippet(transcriptPath string) string {
	if transcriptPath == "" {
		return ""
	}
	entries, _, err := transcript.Tail(transcriptPath, 0)
	if err != nil {
		return ""
	}
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Role == "assistant" && !strings.HasPrefix(entries[i].Text, "→ ") {
			text := entries[i].Text
			if len(text) > 600 {
				text = text[:600] + "…"
			}
			return text
		}
	}
	return ""
}

// runQueue dispatches the next queued prompt for a project that just went
// idle. A short delay lets claude's TUI settle back at the prompt first.
// Concurrent triggers (rapid queue adds, Stop events) race for the same head
// item, so the pick-check-dispatch-mark sequence runs under a lock.
func (s *Server) runQueue(project string) {
	time.Sleep(2 * time.Second)

	s.dispatchMu.Lock()
	defer s.dispatchMu.Unlock()

	if t, ok := s.inFlight[project]; ok && time.Since(t) < inFlightExpiry {
		return // a queued prompt's turn is still running — next Stop retries
	}
	item, err := s.queue.NextFor(project)
	if err != nil || item == nil {
		return
	}
	state, err := s.store.ProjectState(project)
	if err != nil || state != event.StateIdle {
		return // agent busy again (or waiting on a permission) — retry on next Stop
	}
	// Mark before pasting: a lost prompt beats a triple-pasted one, and the
	// user sees undelivered items disappear from the queue either way.
	if err := s.queue.MarkDispatched(item.ID); err != nil {
		return
	}
	if err := tmuxdrv.Dispatch(project, item.Prompt); err != nil {
		log.Printf("queue dispatch %s #%d failed: %v", project, item.ID, err)
		return
	}
	s.inFlight[project] = time.Now()
	s.hub.broadcast("queue", map[string]any{"project": project, "dispatched": item.ID})
	log.Printf("queue: dispatched #%d to %s", item.ID, project)
}

func (s *Server) handleDispatch(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Project string `json:"project"`
		Prompt  string `json:"prompt"`
		Force   bool   `json:"force"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if in.Project == "" || strings.TrimSpace(in.Prompt) == "" {
		http.Error(w, "project and prompt required", http.StatusBadRequest)
		return
	}
	state, err := s.store.ProjectState(in.Project)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if state == event.StateNeedsInput && !in.Force {
		http.Error(w,
			fmt.Sprintf("%s is waiting for a permission decision — answer it first (keystrokes could select an option), or pass force", in.Project),
			http.StatusConflict)
		return
	}
	if err := tmuxdrv.Dispatch(in.Project, in.Prompt); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, map[string]string{"status": "dispatched", "project": in.Project})
}

func (s *Server) handleAddProject(w http.ResponseWriter, r *http.Request) {
	var in struct{ Name, Path string }
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if in.Name == "" || in.Path == "" {
		http.Error(w, "name and path required", http.StatusBadRequest)
		return
	}
	if err := s.store.AddProject(in.Name, in.Path); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	writeJSON(w, map[string]string{"status": "added"})
}

func (s *Server) handleRemoveProject(w http.ResponseWriter, r *http.Request) {
	if err := s.store.RemoveProject(r.PathValue("name")); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]string{"status": "removed"})
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	all := r.URL.Query().Get("all") == "1"
	sessions, err := s.store.ListSessions(!all)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"sessions": sessions})
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
			limit = n
		}
	}
	events, err := s.store.ListEvents(limit, r.URL.Query().Get("project"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"events": events})
}

func (s *Server) handleProjects(w http.ResponseWriter, _ *http.Request) {
	projects, err := s.store.ListProjects()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"projects": projects})
}

// InboxItem is something waiting on the user.
type InboxItem struct {
	Kind      string `json:"kind"` // permission | review
	Project   string `json:"project"`
	SessionID string `json:"session_id"`
	Summary   string `json:"summary"`
	Since     int64  `json:"since"`
}

func (s *Server) handleInbox(w http.ResponseWriter, _ *http.Request) {
	sessions, err := s.store.ListSessions(true)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	items := []InboxItem{}
	now := time.Now().Unix()
	for _, sess := range sessions {
		switch sess.State {
		case event.StateNeedsInput:
			summary := sess.Tool
			if sess.Summary != "" {
				summary = sess.Tool + ": " + sess.Summary
			}
			items = append(items, InboxItem{
				Kind: "permission", Project: sess.Project, SessionID: sess.SessionID,
				Summary: summary, Since: sess.UpdatedAt,
			})
		case event.StateIdle:
			// Long turn finished recently → worth reviewing.
			if now-sess.UpdatedAt > 3600 {
				continue
			}
			started := s.store.TurnStartedAt(sess.SessionID, s.store.LastEventID(sess.SessionID))
			if started > 0 && sess.UpdatedAt-started >= int64(minTurnForDoneAlert.Seconds()) {
				items = append(items, InboxItem{
					Kind: "review", Project: sess.Project, SessionID: sess.SessionID,
					Summary: "finished a task — review the result", Since: sess.UpdatedAt,
				})
			}
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Kind != items[j].Kind {
			return items[i].Kind == "permission" // permissions first
		}
		return items[i].Since < items[j].Since // longest-waiting first
	})
	writeJSON(w, map[string]any{"items": items})
}

func (s *Server) handleTranscript(w http.ResponseWriter, r *http.Request) {
	sess, err := s.store.GetSession(r.PathValue("session_id"))
	if err != nil {
		http.Error(w, "unknown session", http.StatusNotFound)
		return
	}
	if sess.TranscriptPath == "" {
		http.Error(w, "session has no transcript path", http.StatusNotFound)
		return
	}
	var offset int64
	fmt.Sscanf(r.URL.Query().Get("after"), "%d", &offset)
	entries, newOffset, err := transcript.Tail(sess.TranscriptPath, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if entries == nil {
		entries = []transcript.Entry{}
	}
	writeJSON(w, map[string]any{"entries": entries, "offset": newOffset})
}

func (s *Server) handleQueueList(w http.ResponseWriter, r *http.Request) {
	items, err := s.queue.List(r.URL.Query().Get("project"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if items == nil {
		items = []queue.Item{}
	}
	writeJSON(w, map[string]any{"items": items})
}

func (s *Server) handleQueueAdd(w http.ResponseWriter, r *http.Request) {
	var in struct{ Project, Prompt string }
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if in.Project == "" || strings.TrimSpace(in.Prompt) == "" {
		http.Error(w, "project and prompt required", http.StatusBadRequest)
		return
	}
	item, err := s.queue.Enqueue(in.Project, in.Prompt)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// If the agent is already idle, run immediately rather than waiting for
	// the next Stop event.
	go s.runQueue(in.Project)
	writeJSON(w, item)
}

func (s *Server) handleQueueCancel(w http.ResponseWriter, r *http.Request) {
	var id int64
	if _, err := fmt.Sscanf(r.PathValue("id"), "%d", &id); err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	if err := s.queue.Cancel(id); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]string{"status": "cancelled"})
}

func (s *Server) handlePlaybookList(w http.ResponseWriter, _ *http.Request) {
	books, err := s.queue.ListPlaybooks()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if books == nil {
		books = []queue.Playbook{}
	}
	writeJSON(w, map[string]any{"playbooks": books})
}

func (s *Server) handlePlaybookSave(w http.ResponseWriter, r *http.Request) {
	var in struct{ Name, Prompt string }
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if in.Name == "" || strings.TrimSpace(in.Prompt) == "" {
		http.Error(w, "name and prompt required", http.StatusBadRequest)
		return
	}
	if err := s.queue.SavePlaybook(in.Name, in.Prompt); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "saved"})
}

func (s *Server) handlePlaybookDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.queue.DeletePlaybook(r.PathValue("name")); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]string{"status": "deleted"})
}

// handleBroadcast queues a prompt (or playbook) across multiple projects.
func (s *Server) handleBroadcast(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Prompt   string   `json:"prompt"`
		Playbook string   `json:"playbook"`
		Projects []string `json:"projects"`
		All      bool     `json:"all"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if in.Playbook != "" {
		pb, err := s.queue.GetPlaybook(in.Playbook)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		in.Prompt = pb.Prompt
	}
	if strings.TrimSpace(in.Prompt) == "" {
		http.Error(w, "prompt or playbook required", http.StatusBadRequest)
		return
	}
	if in.All {
		projects, err := s.store.ListProjects()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		in.Projects = in.Projects[:0]
		for _, p := range projects {
			in.Projects = append(in.Projects, p.Name)
		}
	}
	if len(in.Projects) == 0 {
		http.Error(w, "projects (or all) required", http.StatusBadRequest)
		return
	}
	queued := []string{}
	for _, p := range in.Projects {
		if _, err := s.queue.Enqueue(p, queue.Render(in.Prompt, p)); err != nil {
			http.Error(w, fmt.Sprintf("enqueue %s: %v", p, err), http.StatusInternalServerError)
			return
		}
		queued = append(queued, p)
		go s.runQueue(p)
	}
	writeJSON(w, map[string]any{"status": "queued", "projects": queued})
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{"projects": s.prober.Results()})
}

func (s *Server) handleCosts(w http.ResponseWriter, r *http.Request) {
	days := 7
	if v := r.URL.Query().Get("days"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 90 {
			days = n
		}
	}
	from := time.Now().AddDate(0, 0, -(days - 1)).Format("2006-01-02")
	rows, err := s.store.UsageSince(from)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if rows == nil {
		rows = []store.UsageRow{}
	}
	writeJSON(w, map[string]any{"usage": rows})
}

func (s *Server) handleDigest(w http.ResponseWriter, r *http.Request) {
	day := time.Now().Format("2006-01-02")
	if v := r.URL.Query().Get("day"); v != "" {
		if _, err := time.Parse("2006-01-02", v); err != nil {
			http.Error(w, "day must be YYYY-MM-DD", http.StatusBadRequest)
			return
		}
		day = v
	}
	start, _ := time.ParseInLocation("2006-01-02", day, time.Local)
	activity, err := s.store.ActivityBetween(start.Unix(), start.AddDate(0, 0, 1).Unix())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	usage, err := s.store.UsageSince(day)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	usageByProject := map[string]store.UsageRow{}
	for _, u := range usage {
		if u.Day == day {
			usageByProject[u.Project] = u
		}
	}
	type digestRow struct {
		store.DayActivity
		OutputTokens int64 `json:"output_tokens"`
		InputTokens  int64 `json:"input_tokens"`
	}
	rows := []digestRow{}
	for _, a := range activity {
		u := usageByProject[a.Project]
		rows = append(rows, digestRow{DayActivity: a, OutputTokens: u.OutputTokens, InputTokens: u.InputTokens})
	}
	writeJSON(w, map[string]any{"day": day, "projects": rows})
}

func (s *Server) handleSetPorts(w http.ResponseWriter, r *http.Request) {
	var in struct{ Ports string }
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.store.SetProjectPorts(r.PathValue("name"), in.Ports); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]string{"status": "updated"})
}

// spaHandler serves the embedded dashboard, falling back to index.html for
// client-side routes.
func spaHandler(webFS fs.FS) http.Handler {
	fileServer := http.FileServerFS(webFS)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path != "" {
			if f, err := webFS.Open(path); err == nil {
				f.Close()
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}

// reconcile self-heals sessions stuck in needs_input/working: permission
// denials and user interrupts append to the transcript but fire no Stop
// hook, so the state machine never hears the turn end. When the transcript
// advanced past the last hook event and has then been quiet for a minute,
// the turn is over — downgrade to idle.
func (s *Server) reconcile() {
	const quiet = 60 * time.Second
	for range time.Tick(30 * time.Second) {
		sessions, err := s.store.ListSessions(true)
		if err != nil {
			continue
		}
		for _, sess := range sessions {
			if sess.State != event.StateNeedsInput && sess.State != event.StateWorking {
				continue
			}
			if sess.TranscriptPath == "" {
				continue
			}
			fi, err := os.Stat(sess.TranscriptPath)
			if err != nil {
				continue
			}
			mtime := fi.ModTime()
			if mtime.Unix() <= sess.UpdatedAt || time.Since(mtime) < quiet {
				continue
			}
			if err := s.store.ForceState(sess.SessionID, event.StateIdle); err != nil {
				continue
			}
			log.Printf("reconcile: %s (%s) %s → idle (transcript moved on without a Stop)",
				sess.Project, sess.SessionID[:8], sess.State)
			if updated, err := s.store.GetSession(sess.SessionID); err == nil {
				s.hub.broadcast("session", updated)
				if state, err := s.store.ProjectState(updated.Project); err == nil {
					tmuxdrv.SetIcon(updated.Project, state)
				}
			}
		}
	}
}

// drainSpool ingests events spooled while the daemon was down.
func (s *Server) drainSpool() {
	entries, err := os.ReadDir(s.cfg.SpoolDir)
	if err != nil {
		return
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(s.cfg.SpoolDir, e.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var ev event.Event
		if err := json.Unmarshal(raw, &ev); err == nil {
			if _, err := s.apply(&ev); err != nil {
				log.Printf("spool apply %s: %v", e.Name(), err)
				continue
			}
			n++
		}
		os.Remove(path)
	}
	if n > 0 {
		log.Printf("drained %d spooled events", n)
	}
}

// watchSpool periodically drains stragglers (hook races around daemon start).
func (s *Server) watchSpool() {
	for range time.Tick(5 * time.Second) {
		s.drainSpool()
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// --- SSE hub ---

type hub struct {
	mu   sync.Mutex
	subs map[chan []byte]struct{}
}

func newHub() *hub {
	return &hub{subs: map[chan []byte]struct{}{}}
}

func (h *hub) broadcast(kind string, payload any) {
	msg, err := json.Marshal(map[string]any{"type": kind, "data": payload})
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- msg:
		default: // slow subscriber: drop rather than block ingestion
		}
	}
}

func (h *hub) serveSSE(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	ch := make(chan []byte, 64)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.subs, ch)
		h.mu.Unlock()
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	fmt.Fprintf(w, ": connected\n\n")
	fl.Flush()

	keepalive := time.NewTicker(25 * time.Second)
	defer keepalive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-ch:
			fmt.Fprintf(w, "data: %s\n\n", msg)
			fl.Flush()
		case <-keepalive.C:
			fmt.Fprintf(w, ": ping\n\n")
			fl.Flush()
		}
	}
}
