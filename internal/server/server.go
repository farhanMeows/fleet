// Package server implements the fleet daemon: event ingestion, REST API,
// and SSE stream for live dashboard updates.
package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/farhanahmad/fleet/internal/config"
	"github.com/farhanahmad/fleet/internal/event"
	"github.com/farhanahmad/fleet/internal/store"
)

type Server struct {
	cfg   *config.Config
	store *store.Store
	hub   *hub
}

func New(cfg *config.Config, st *store.Store) *Server {
	return &Server{cfg: cfg, store: st, hub: newHub()}
}

// Run drains the spool, then serves until the process exits.
func (s *Server) Run() error {
	s.drainSpool()
	go s.watchSpool()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/hook", s.handleHook)
	mux.HandleFunc("GET /api/sessions", s.handleSessions)
	mux.HandleFunc("GET /api/events", s.handleEvents)
	mux.HandleFunc("GET /api/projects", s.handleProjects)
	mux.HandleFunc("GET /api/stream", s.hub.serveSSE)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, "ok")
	})

	addr := fmt.Sprintf("127.0.0.1:%d", s.cfg.Port)
	log.Printf("fleet daemon listening on http://%s", addr)
	return http.ListenAndServe(addr, mux)
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
	return sess, nil
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
