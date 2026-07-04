// Package queue implements per-project task queues, reusable playbooks, and
// cross-project broadcast. The daemon's queue runner dispatches the next
// queued prompt when a project's agent goes idle.
package queue

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Item states: queued → dispatched (terminal: cancelled).
const (
	StatusQueued     = "queued"
	StatusDispatched = "dispatched"
	StatusCancelled  = "cancelled"
)

type Item struct {
	ID           int64  `json:"id"`
	Project      string `json:"project"`
	Prompt       string `json:"prompt"`
	Status       string `json:"status"`
	CreatedAt    int64  `json:"created_at"`
	DispatchedAt int64  `json:"dispatched_at,omitempty"`
}

type Playbook struct {
	Name      string `json:"name"`
	Prompt    string `json:"prompt"`
	UpdatedAt int64  `json:"updated_at"`
}

type Queue struct {
	db *sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS queue_items (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	project       TEXT NOT NULL,
	prompt        TEXT NOT NULL,
	status        TEXT NOT NULL DEFAULT 'queued',
	created_at    INTEGER NOT NULL,
	dispatched_at INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_queue_project ON queue_items(project, status, id);
CREATE TABLE IF NOT EXISTS playbooks (
	name       TEXT PRIMARY KEY,
	prompt     TEXT NOT NULL,
	updated_at INTEGER NOT NULL
);
`

func New(db *sql.DB) (*Queue, error) {
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("queue migrate: %w", err)
	}
	return &Queue{db: db}, nil
}

func (q *Queue) Enqueue(project, prompt string) (*Item, error) {
	res, err := q.db.Exec(
		`INSERT INTO queue_items(project, prompt, status, created_at) VALUES(?,?,?,?)`,
		project, prompt, StatusQueued, time.Now().Unix())
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return q.get(id)
}

// NextFor returns the oldest queued item for a project, or nil.
func (q *Queue) NextFor(project string) (*Item, error) {
	row := q.db.QueryRow(
		`SELECT id FROM queue_items WHERE project = ? AND status = ? ORDER BY id LIMIT 1`,
		project, StatusQueued)
	var id int64
	if err := row.Scan(&id); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return q.get(id)
}

func (q *Queue) MarkDispatched(id int64) error {
	_, err := q.db.Exec(`UPDATE queue_items SET status = ?, dispatched_at = ? WHERE id = ?`,
		StatusDispatched, time.Now().Unix(), id)
	return err
}

func (q *Queue) Cancel(id int64) error {
	res, err := q.db.Exec(`UPDATE queue_items SET status = ? WHERE id = ? AND status = ?`,
		StatusCancelled, id, StatusQueued)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("no queued item #%d", id)
	}
	return nil
}

// List returns queued items (all projects, or one when project != "").
func (q *Queue) List(project string) ([]Item, error) {
	query := `SELECT id, project, prompt, status, created_at, dispatched_at FROM queue_items WHERE status = ?`
	args := []any{StatusQueued}
	if project != "" {
		query += ` AND project = ?`
		args = append(args, project)
	}
	query += ` ORDER BY project, id`
	rows, err := q.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Item
	for rows.Next() {
		var it Item
		if err := rows.Scan(&it.ID, &it.Project, &it.Prompt, &it.Status, &it.CreatedAt, &it.DispatchedAt); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func (q *Queue) get(id int64) (*Item, error) {
	row := q.db.QueryRow(
		`SELECT id, project, prompt, status, created_at, dispatched_at FROM queue_items WHERE id = ?`, id)
	var it Item
	if err := row.Scan(&it.ID, &it.Project, &it.Prompt, &it.Status, &it.CreatedAt, &it.DispatchedAt); err != nil {
		return nil, err
	}
	return &it, nil
}

// --- playbooks ---

func (q *Queue) SavePlaybook(name, prompt string) error {
	_, err := q.db.Exec(
		`INSERT INTO playbooks(name, prompt, updated_at) VALUES(?,?,?)
		 ON CONFLICT(name) DO UPDATE SET prompt = excluded.prompt, updated_at = excluded.updated_at`,
		name, prompt, time.Now().Unix())
	return err
}

func (q *Queue) GetPlaybook(name string) (*Playbook, error) {
	row := q.db.QueryRow(`SELECT name, prompt, updated_at FROM playbooks WHERE name = ?`, name)
	var p Playbook
	if err := row.Scan(&p.Name, &p.Prompt, &p.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("no playbook named %q", name)
		}
		return nil, err
	}
	return &p, nil
}

func (q *Queue) ListPlaybooks() ([]Playbook, error) {
	rows, err := q.db.Query(`SELECT name, prompt, updated_at FROM playbooks ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Playbook
	for rows.Next() {
		var p Playbook
		if err := rows.Scan(&p.Name, &p.Prompt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (q *Queue) DeletePlaybook(name string) error {
	res, err := q.db.Exec(`DELETE FROM playbooks WHERE name = ?`, name)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("no playbook named %q", name)
	}
	return nil
}

// Render substitutes {{project}} in a playbook prompt.
func Render(prompt, project string) string {
	return strings.ReplaceAll(prompt, "{{project}}", project)
}
