// Package store persists fleet state in SQLite (pure-Go driver, no CGO).
package store

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/farhanahmad/fleet/internal/event"
)

type Store struct {
	db *sql.DB
}

type Project struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	Ports     string `json:"ports,omitempty"` // comma-separated dev-server ports to health-check
	CreatedAt int64  `json:"created_at"`
}

type Session struct {
	SessionID      string `json:"session_id"`
	Project        string `json:"project"`
	Cwd            string `json:"cwd"`
	TranscriptPath string `json:"transcript_path,omitempty"`
	State          string `json:"state"`
	Tool           string `json:"tool,omitempty"`
	Summary        string `json:"summary,omitempty"`
	StartedAt      int64  `json:"started_at"`
	UpdatedAt      int64  `json:"updated_at"`
}

type EventRow struct {
	ID        int64  `json:"id"`
	SessionID string `json:"session_id"`
	Project   string `json:"project"`
	Event     string `json:"event"`
	Tool      string `json:"tool,omitempty"`
	Summary   string `json:"summary,omitempty"`
	Cwd       string `json:"cwd"`
	CreatedAt int64  `json:"created_at"`
}

const schema = `
CREATE TABLE IF NOT EXISTS projects (
	name       TEXT PRIMARY KEY,
	path       TEXT NOT NULL UNIQUE,
	created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS sessions (
	session_id      TEXT PRIMARY KEY,
	project         TEXT NOT NULL,
	cwd             TEXT NOT NULL,
	transcript_path TEXT NOT NULL DEFAULT '',
	state           TEXT NOT NULL,
	tool            TEXT NOT NULL DEFAULT '',
	summary         TEXT NOT NULL DEFAULT '',
	started_at      INTEGER NOT NULL,
	updated_at      INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS events (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	session_id TEXT NOT NULL,
	project    TEXT NOT NULL,
	event      TEXT NOT NULL,
	tool       TEXT NOT NULL DEFAULT '',
	summary    TEXT NOT NULL DEFAULT '',
	cwd        TEXT NOT NULL,
	created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_events_session ON events(session_id, id);
CREATE INDEX IF NOT EXISTS idx_events_created ON events(created_at);
CREATE TABLE IF NOT EXISTS usage_daily (
	project      TEXT NOT NULL,
	day          TEXT NOT NULL,
	input_tokens  INTEGER NOT NULL DEFAULT 0,
	output_tokens INTEGER NOT NULL DEFAULT 0,
	cache_read    INTEGER NOT NULL DEFAULT 0,
	cache_create  INTEGER NOT NULL DEFAULT 0,
	turns         INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (project, day)
);
`

// idempotent column additions for databases created by earlier versions
var migrations = []string{
	`ALTER TABLE projects ADD COLUMN ports TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE sessions ADD COLUMN usage_offset INTEGER NOT NULL DEFAULT 0`,
}

func Open(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	// The daemon is the only writer; a single connection avoids SQLITE_BUSY.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	for _, m := range migrations {
		db.Exec(m) // "duplicate column" on already-migrated databases is fine
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// DB exposes the shared handle for sibling packages (queue) that keep their
// own tables in the same database.
func (s *Store) DB() *sql.DB { return s.db }

// --- project registry ---

func (s *Store) AddProject(name, path string) error {
	_, err := s.db.Exec(`INSERT INTO projects(name, path, created_at) VALUES(?,?,?)`,
		name, filepath.Clean(path), time.Now().Unix())
	return err
}

func (s *Store) RemoveProject(name string) error {
	res, err := s.db.Exec(`DELETE FROM projects WHERE name = ?`, name)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("no project named %q", name)
	}
	return nil
}

func (s *Store) ListProjects() ([]Project, error) {
	rows, err := s.db.Query(`SELECT name, path, ports, created_at FROM projects ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Project
	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.Name, &p.Path, &p.Ports, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) SetProjectPorts(name, ports string) error {
	res, err := s.db.Exec(`UPDATE projects SET ports = ? WHERE name = ?`, ports, name)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("no project named %q", name)
	}
	return nil
}

// --- token usage ---

// UsageOffset returns the transcript byte offset already accounted for.
func (s *Store) UsageOffset(sessionID string) int64 {
	var off int64
	s.db.QueryRow(`SELECT usage_offset FROM sessions WHERE session_id = ?`, sessionID).Scan(&off)
	return off
}

func (s *Store) SetUsageOffset(sessionID string, offset int64) error {
	_, err := s.db.Exec(`UPDATE sessions SET usage_offset = ? WHERE session_id = ?`, offset, sessionID)
	return err
}

// AddUsage accumulates token usage into the project's daily bucket.
func (s *Store) AddUsage(project, day string, input, output, cacheRead, cacheCreate, turns int64) error {
	_, err := s.db.Exec(`
		INSERT INTO usage_daily(project, day, input_tokens, output_tokens, cache_read, cache_create, turns)
		VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(project, day) DO UPDATE SET
			input_tokens = input_tokens + excluded.input_tokens,
			output_tokens = output_tokens + excluded.output_tokens,
			cache_read = cache_read + excluded.cache_read,
			cache_create = cache_create + excluded.cache_create,
			turns = turns + excluded.turns`,
		project, day, input, output, cacheRead, cacheCreate, turns)
	return err
}

type UsageRow struct {
	Project      string `json:"project"`
	Day          string `json:"day"`
	InputTokens  int64  `json:"input_tokens"`
	OutputTokens int64  `json:"output_tokens"`
	CacheRead    int64  `json:"cache_read"`
	CacheCreate  int64  `json:"cache_create"`
	Turns        int64  `json:"turns"`
}

// UsageSince returns daily usage rows for days >= fromDay (YYYY-MM-DD).
func (s *Store) UsageSince(fromDay string) ([]UsageRow, error) {
	rows, err := s.db.Query(`
		SELECT project, day, input_tokens, output_tokens, cache_read, cache_create, turns
		FROM usage_daily WHERE day >= ? ORDER BY day DESC, project`, fromDay)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UsageRow
	for rows.Next() {
		var r UsageRow
		if err := rows.Scan(&r.Project, &r.Day, &r.InputTokens, &r.OutputTokens, &r.CacheRead, &r.CacheCreate, &r.Turns); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// DayActivity summarizes a project's events for one day (unix range).
type DayActivity struct {
	Project    string `json:"project"`
	Sessions   int64  `json:"sessions"`
	Turns      int64  `json:"turns"`
	ToolEvents int64  `json:"tool_events"`
}

func (s *Store) ActivityBetween(from, to int64) ([]DayActivity, error) {
	rows, err := s.db.Query(`
		SELECT project,
		       COUNT(DISTINCT session_id),
		       SUM(CASE WHEN event = 'Stop' THEN 1 ELSE 0 END),
		       SUM(CASE WHEN event = 'PreToolUse' THEN 1 ELSE 0 END)
		FROM events WHERE created_at >= ? AND created_at < ?
		GROUP BY project ORDER BY project`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DayActivity
	for rows.Next() {
		var a DayActivity
		if err := rows.Scan(&a.Project, &a.Sessions, &a.Turns, &a.ToolEvents); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ResolveProject maps a session cwd to a registered project name using the
// longest path-prefix match. Unregistered cwds get a name derived from the
// path so they still show up in the dashboard (as "unregistered" projects).
func (s *Store) ResolveProject(cwd string) (name string, registered bool) {
	cwd = filepath.Clean(cwd)
	row := s.db.QueryRow(
		`SELECT name FROM projects WHERE ? = path OR ? LIKE path || '/%' ORDER BY length(path) DESC LIMIT 1`,
		cwd, cwd)
	if err := row.Scan(&name); err == nil {
		return name, true
	}
	return filepath.Base(cwd), false
}

// --- event ingestion ---

// ApplyEvent records the event and updates the derived session row.
func (s *Store) ApplyEvent(ev *event.Event) (*Session, error) {
	project, _ := s.ResolveProject(ev.Cwd)
	_, err := s.db.Exec(
		`INSERT INTO events(session_id, project, event, tool, summary, cwd, created_at) VALUES(?,?,?,?,?,?,?)`,
		ev.SessionID, project, ev.Event, ev.ToolName, ev.Summary, ev.Cwd, ev.ReceivedAt)
	if err != nil {
		return nil, err
	}

	state := event.StateFor(ev.Event)
	if ev.Event == event.SessionEnd {
		_, err = s.db.Exec(`UPDATE sessions SET state = ?, updated_at = ? WHERE session_id = ?`,
			event.StateEnded, ev.ReceivedAt, ev.SessionID)
		if err != nil {
			return nil, err
		}
		return s.GetSession(ev.SessionID)
	}

	_, err = s.db.Exec(`
		INSERT INTO sessions(session_id, project, cwd, transcript_path, state, tool, summary, started_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?)
		ON CONFLICT(session_id) DO UPDATE SET
			project = excluded.project,
			cwd = excluded.cwd,
			transcript_path = CASE WHEN excluded.transcript_path != '' THEN excluded.transcript_path ELSE sessions.transcript_path END,
			state = CASE WHEN excluded.state != '' THEN excluded.state ELSE sessions.state END,
			tool = excluded.tool,
			summary = excluded.summary,
			updated_at = excluded.updated_at`,
		ev.SessionID, project, ev.Cwd, ev.TranscriptPath, state, ev.ToolName, ev.Summary, ev.ReceivedAt, ev.ReceivedAt)
	if err != nil {
		return nil, err
	}
	return s.GetSession(ev.SessionID)
}

func (s *Store) GetSession(id string) (*Session, error) {
	row := s.db.QueryRow(
		`SELECT session_id, project, cwd, transcript_path, state, tool, summary, started_at, updated_at
		 FROM sessions WHERE session_id = ?`, id)
	var sess Session
	if err := row.Scan(&sess.SessionID, &sess.Project, &sess.Cwd, &sess.TranscriptPath,
		&sess.State, &sess.Tool, &sess.Summary, &sess.StartedAt, &sess.UpdatedAt); err != nil {
		return nil, err
	}
	return &sess, nil
}

// ListSessions returns sessions, most recently active first. When activeOnly
// is set, ended sessions and sessions stale for over six hours are excluded.
func (s *Store) ListSessions(activeOnly bool) ([]Session, error) {
	q := `SELECT session_id, project, cwd, transcript_path, state, tool, summary, started_at, updated_at
	      FROM sessions`
	var args []any
	if activeOnly {
		q += ` WHERE state != ? AND updated_at > ?`
		args = append(args, event.StateEnded, time.Now().Add(-6*time.Hour).Unix())
	}
	q += ` ORDER BY updated_at DESC`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		var sess Session
		if err := rows.Scan(&sess.SessionID, &sess.Project, &sess.Cwd, &sess.TranscriptPath,
			&sess.State, &sess.Tool, &sess.Summary, &sess.StartedAt, &sess.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, sess)
	}
	return out, rows.Err()
}

// ProjectState returns the worst state across a project's active sessions
// (needs_input > working > idle), or "" if the project has none.
func (s *Store) ProjectState(project string) (string, error) {
	sessions, err := s.ListSessions(true)
	if err != nil {
		return "", err
	}
	worst := ""
	rank := map[string]int{event.StateIdle: 1, event.StateWorking: 2, event.StateNeedsInput: 3}
	for _, sess := range sessions {
		if sess.Project == project && rank[sess.State] > rank[worst] {
			worst = sess.State
		}
	}
	return worst, nil
}

// TurnStartedAt returns when the session's current turn began: the time of
// the most recent Stop or SessionStart event strictly before the given event
// id. Zero when unknown.
func (s *Store) TurnStartedAt(sessionID string, beforeID int64) int64 {
	row := s.db.QueryRow(
		`SELECT created_at FROM events
		 WHERE session_id = ? AND id < ? AND event IN ('Stop','SessionStart')
		 ORDER BY id DESC LIMIT 1`, sessionID, beforeID)
	var t int64
	if err := row.Scan(&t); err != nil {
		return 0
	}
	return t
}

// LastEventID returns the newest event id for a session (0 if none).
func (s *Store) LastEventID(sessionID string) int64 {
	row := s.db.QueryRow(`SELECT COALESCE(MAX(id),0) FROM events WHERE session_id = ?`, sessionID)
	var id int64
	row.Scan(&id)
	return id
}

func (s *Store) ListEvents(limit int, project string) ([]EventRow, error) {
	q := `SELECT id, session_id, project, event, tool, summary, cwd, created_at FROM events`
	var args []any
	if project != "" {
		q += ` WHERE project = ?`
		args = append(args, project)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EventRow
	for rows.Next() {
		var e EventRow
		if err := rows.Scan(&e.ID, &e.SessionID, &e.Project, &e.Event, &e.Tool, &e.Summary, &e.Cwd, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// SuggestName derives a project name from a path (basename, lowercased).
func SuggestName(path string) string {
	return strings.ToLower(filepath.Base(filepath.Clean(path)))
}
