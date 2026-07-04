package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"

	"github.com/farhanahmad/fleet/internal/client"
	"github.com/farhanahmad/fleet/internal/event"
	"github.com/farhanahmad/fleet/internal/queue"
	"github.com/farhanahmad/fleet/internal/store"
	"github.com/farhanahmad/fleet/internal/tmuxdrv"
)

const watchInterval = 2 * time.Second

var (
	wsTitle    = lipgloss.NewStyle().Bold(true)
	wsDim      = lipgloss.NewStyle().Faint(true)
	wsSelected = lipgloss.NewStyle().Reverse(true).Bold(true)
	wsErr      = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	wsOK       = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))

	wsState = map[string]lipgloss.Style{
		event.StateWorking:    lipgloss.NewStyle().Foreground(lipgloss.Color("3")),
		event.StateNeedsInput: lipgloss.NewStyle().Foreground(lipgloss.Color("1")),
		event.StateIdle:       lipgloss.NewStyle().Foreground(lipgloss.Color("2")),
		"":                    wsDim,
	}
	wsIcon = map[string]string{
		event.StateWorking:    "●",
		event.StateNeedsInput: "⚠",
		event.StateIdle:       "✓",
		"":                    "○",
	}
)

type watchRow struct {
	name       string
	registered bool
	state      string
	tool       string
	summary    string
	updatedAt  int64
	tail       []string // recent activity lines, shown for active projects
	tokIn      int64    // today's tokens
	tokOut     int64
	branch     string // git branch (+dirty count) from the health prober
	dirty      int
	srvUp      int // dev-server ports up / total configured
	srvTotal   int
}

type (
	tickMsg time.Time
	dataMsg struct {
		rows          []watchRow
		tokIn, tokOut int64 // fleet-wide today
		events        []store.EventRow
		queued        []queue.Item
		window        *client.ClaudeWindow
		results       []client.ResultRow
	}
	errMsg   struct{ err error }
	flashMsg struct {
		text  string
		isErr bool
	}
)

type watchModel struct {
	client *client.Client

	rows   []watchRow
	cursor int
	width  int
	height int

	fetchErr      error
	tokIn, tokOut int64 // fleet-wide today
	events        []store.EventRow
	queued        []queue.Item
	window        *client.ClaudeWindow
	results       []client.ResultRow
	showEvents    bool // bottom panel: false = RESULTS (default), true = raw events
	flash         flashMsg
	flashAt       time.Time

	dispatching bool
	input       textinput.Model
}

func newWatchModel(c *client.Client) watchModel {
	ti := textinput.New()
	ti.Prompt = ""
	ti.CharLimit = 0
	return watchModel{client: c, input: ti}
}

func (m watchModel) Init() tea.Cmd {
	return tea.Batch(fetchRows(m.client), tick())
}

func tick() tea.Cmd {
	return tea.Tick(watchInterval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func fetchRows(c *client.Client) tea.Cmd {
	return func() tea.Msg {
		projects, err := c.Projects()
		if err != nil {
			return errMsg{err}
		}
		sessions, err := c.Sessions()
		if err != nil {
			return errMsg{err}
		}
		// Best-effort extras: activity tails, tokens, health, queue, 5h window.
		events, _ := c.Events(80)
		usage, _ := c.Costs(1)
		health, _ := c.Health()
		queued, _ := c.QueueList("")
		window, _ := c.ClaudeWindow()
		results, _ := c.Results()
		rows := buildRows(projects, sessions)
		attachTails(rows, events)
		var totIn, totOut int64
		byProject := map[string]store.UsageRow{}
		for _, u := range usage {
			byProject[u.Project] = u
			totIn += u.InputTokens
			totOut += u.OutputTokens
		}
		for i := range rows {
			rows[i].tokIn = byProject[rows[i].name].InputTokens
			rows[i].tokOut = byProject[rows[i].name].OutputTokens
			if h, ok := health[rows[i].name]; ok {
				rows[i].branch, rows[i].dirty = h.GitBranch, h.GitDirty
				for _, p := range h.Ports {
					rows[i].srvTotal++
					if p.Open {
						rows[i].srvUp++
					}
				}
			}
		}
		return dataMsg{rows: rows, tokIn: totIn, tokOut: totOut,
			events: events, queued: queued, window: window, results: results}
	}
}

// attachTails gives active (working / needs-input) rows their last few
// activity lines, oldest first.
func attachTails(rows []watchRow, events []store.EventRow) {
	const tailLen = 3
	byProject := map[string][]string{}
	for i := len(events) - 1; i >= 0; i-- { // events arrive newest-first
		e := events[i]
		if e.Event != event.PreToolUse && e.Event != event.PermissionRequest {
			continue
		}
		line := time.Unix(e.CreatedAt, 0).Format("15:04:05") + "  " + e.Tool
		if e.Summary != "" {
			line += ": " + e.Summary
		}
		if e.Event == event.PermissionRequest {
			line += "  ⚠ awaiting approval"
		}
		byProject[e.Project] = append(byProject[e.Project], line)
	}
	for i := range rows {
		if rows[i].state != event.StateWorking && rows[i].state != event.StateNeedsInput {
			continue
		}
		tail := byProject[rows[i].name]
		if len(tail) > tailLen {
			tail = tail[len(tail)-tailLen:]
		}
		rows[i].tail = tail
	}
}

func buildRows(projects []store.Project, sessions []store.Session) []watchRow {
	byProject := map[string][]store.Session{}
	for _, s := range sessions {
		byProject[s.Project] = append(byProject[s.Project], s)
	}
	rows := make([]watchRow, 0, len(projects))
	registered := map[string]bool{}
	for _, p := range projects {
		registered[p.Name] = true
		state, tool, summary, updatedAt := worstOf(byProject[p.Name])
		rows = append(rows, watchRow{
			name: p.Name, registered: true,
			state: state, tool: tool, summary: summary, updatedAt: updatedAt,
		})
	}
	var extra []string
	for name := range byProject {
		if !registered[name] {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	for _, name := range extra {
		state, tool, summary, updatedAt := worstOf(byProject[name])
		rows = append(rows, watchRow{
			name: name, registered: false,
			state: state, tool: tool, summary: summary, updatedAt: updatedAt,
		})
	}
	return rows
}

func (m watchModel) selected() *watchRow {
	if m.cursor >= 0 && m.cursor < len(m.rows) {
		return &m.rows[m.cursor]
	}
	return nil
}

func (m watchModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.input.Width = max(10, m.width-24)
		return m, nil

	case tickMsg:
		if m.flash.text != "" && time.Since(m.flashAt) > 5*time.Second {
			m.flash = flashMsg{}
		}
		return m, tea.Batch(fetchRows(m.client), tick())

	case dataMsg:
		m.rows = msg.rows
		m.tokIn, m.tokOut = msg.tokIn, msg.tokOut
		m.events, m.queued, m.window = msg.events, msg.queued, msg.window
		m.results = msg.results
		m.fetchErr = nil
		if m.cursor >= len(m.rows) {
			m.cursor = len(m.rows) - 1
		}
		if m.cursor < 0 {
			m.cursor = 0
		}
		return m, nil

	case errMsg:
		m.fetchErr = msg.err // keep the last good rows on screen
		return m, nil

	case flashMsg:
		m.flash = msg
		m.flashAt = time.Now()
		return m, nil

	case tea.KeyMsg:
		if m.dispatching {
			return m.updateDispatch(msg)
		}
		return m.updateBrowse(msg)
	}
	return m, nil
}

func (m watchModel) updateBrowse(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c", "esc":
		return m, tea.Quit
	case "j", "down":
		if m.cursor < len(m.rows)-1 {
			m.cursor++
		}
	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
		}
	case "g", "home":
		m.cursor = 0
	case "G", "end":
		if len(m.rows) > 0 {
			m.cursor = len(m.rows) - 1
		}
	case "r":
		return m, fetchRows(m.client)
	case "e":
		m.showEvents = !m.showEvents
	case "enter":
		if row := m.selected(); row != nil {
			return m, jumpTo(row.name)
		}
	case "d":
		if row := m.selected(); row != nil {
			m.dispatching = true
			m.input.SetValue("")
			m.input.Width = max(10, m.width-runewidth.StringWidth(row.name)-18)
			m.input.Focus()
			return m, textinput.Blink
		}
	}
	return m, nil
}

func (m watchModel) updateDispatch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.dispatching = false
		m.input.Blur()
		return m, nil
	case "enter":
		prompt := strings.TrimSpace(m.input.Value())
		row := m.selected()
		m.dispatching = false
		m.input.Blur()
		if prompt == "" || row == nil {
			return m, nil
		}
		return m, dispatchTo(m.client, row.name, prompt)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func jumpTo(project string) tea.Cmd {
	return func() tea.Msg {
		if err := tmuxdrv.Jump(project); err != nil {
			return flashMsg{err.Error(), true}
		}
		return flashMsg{"switched to " + project, false}
	}
}

func dispatchTo(c *client.Client, project, prompt string) tea.Cmd {
	return func() tea.Msg {
		if err := c.Dispatch(project, prompt, false); err != nil {
			return flashMsg{err.Error(), true}
		}
		return flashMsg{"dispatched to " + project, false}
	}
}

// cell truncates (with ellipsis) or pads s to exactly w display columns.
func cell(s string, w int) string {
	if w <= 0 {
		return ""
	}
	return runewidth.FillRight(runewidth.Truncate(s, w, "…"), w)
}

// layout splits the terminal width into column widths. NOW absorbs slack;
// on narrow terminals BRANCH/SRV drop first, then NAME shrinks, then AGE
// and NOW drop entirely.
func (m watchModel) layout() (nameW, stateW, branchW, srvW, nowW, ageW int) {
	nameW, stateW, ageW = 24, 10, 5
	if m.width >= 110 {
		branchW, srvW = 16, 5
	}
	// indent(2) + icon(2) + inter-column gaps (2 each)
	gaps := 6
	if branchW > 0 {
		gaps += 2
	}
	if srvW > 0 {
		gaps += 2
	}
	nowW = m.width - nameW - stateW - branchW - srvW - ageW - 4 - gaps
	if nowW < 10 {
		nameW += nowW - 10
		nowW = 10
	}
	if nameW < 12 {
		nameW = 12
	}
	if m.width < nameW+stateW+ageW+nowW+10 {
		nowW = 0
	}
	if m.width < nameW+stateW+ageW+8 {
		ageW = 0
	}
	return nameW, stateW, branchW, srvW, nowW, ageW
}

func (m watchModel) renderRow(row watchRow, selected bool, nameW, stateW, branchW, srvW, nowW, ageW int) string {
	now := row.tool
	if row.summary != "" {
		now = row.tool + ": " + row.summary
	}
	var b strings.Builder
	b.WriteString("  ")
	b.WriteString(cell(wsIcon[row.state], 2))
	b.WriteString(cell(row.name, nameW))
	b.WriteString("  ")
	b.WriteString(cell(stateLabel(row.state), stateW))
	if branchW > 0 {
		br := row.branch
		if br != "" && row.dirty > 0 {
			br += fmt.Sprintf(" ±%d", row.dirty)
		}
		b.WriteString("  ")
		b.WriteString(cell(br, branchW))
	}
	if srvW > 0 {
		srv := ""
		if row.srvTotal > 0 {
			srv = fmt.Sprintf("%d/%d↑", row.srvUp, row.srvTotal)
		}
		b.WriteString("  ")
		b.WriteString(cell(srv, srvW))
	}
	if nowW > 0 {
		b.WriteString("  ")
		b.WriteString(cell(now, nowW))
	}
	if ageW > 0 {
		b.WriteString("  ")
		b.WriteString(runewidth.FillLeft(humanAge(row.updatedAt), ageW))
	}
	line := b.String()
	if selected {
		return wsSelected.Render(line)
	}
	// Color only the icon..state span (like the static table); Render wraps
	// the already-padded text, so column math stays plain.
	span := 2 + 2 + nameW + 2 + stateW
	head := runewidth.Truncate(line, span, "")
	return wsState[row.state].Render(head) + line[len(head):]
}

// claudeLine renders the estimated 5h usage-window bar with reset countdown.
func (m watchModel) claudeLine() string {
	w := m.window
	if w == nil {
		return ""
	}
	if !w.Active {
		return wsDim.Render("claude  5h window idle — a new window starts with your next prompt")
	}
	until := time.Until(time.Unix(w.ResetsAt, 0)).Round(time.Minute)
	reset := fmt.Sprintf("resets %s (in %dh%02dm)",
		time.Unix(w.ResetsAt, 0).Format("15:04"), int(until.Hours()), int(until.Minutes())%60)
	used := w.InputTokens + w.OutputTokens
	if w.Budget > 0 {
		pct := float64(used) / float64(w.Budget)
		if pct > 1 {
			pct = 1
		}
		const barW = 24
		filled := int(pct * barW)
		bar := strings.Repeat("█", filled) + strings.Repeat("░", barW-filled)
		style := wsOK
		if pct > 0.8 {
			style = wsErr
		} else if pct > 0.5 {
			style = wsState[event.StateWorking]
		}
		return wsDim.Render("claude  ") + style.Render(bar) +
			wsDim.Render(fmt.Sprintf("  %d%% of %s · %s", int(pct*100), humanTokens(w.Budget), reset))
	}
	return wsDim.Render(fmt.Sprintf("claude  5h window: %s in / %s out · %d turns · %s",
		humanTokens(w.InputTokens), humanTokens(w.OutputTokens), w.Turns, reset))
}

func (m watchModel) View() string {
	if m.width == 0 {
		return ""
	}
	nameW, stateW, branchW, srvW, nowW, ageW := m.layout()

	var b strings.Builder
	header := wsTitle.Render("FLEET") + "  " + wsDim.Render(time.Now().Format("15:04:05"))
	if m.tokIn > 0 || m.tokOut > 0 {
		header += "  " + wsDim.Render("· today "+humanTokens(m.tokIn)+" in / "+humanTokens(m.tokOut)+" out")
	}
	if m.fetchErr != nil {
		header += "  " + wsErr.Render("⚠ "+m.fetchErr.Error())
	}
	b.WriteString(truncLine(header, m.width) + "\n")
	headerLines := 3
	if cl := m.claudeLine(); cl != "" {
		b.WriteString(truncLine(cl, m.width) + "\n")
		headerLines++
	}
	b.WriteString("\n")

	head := "  " + cell("", 2) + cell("PROJECT", nameW) + "  " + cell("STATE", stateW)
	if branchW > 0 {
		head += "  " + cell("BRANCH", branchW)
	}
	if srvW > 0 {
		head += "  " + cell("SRV", srvW)
	}
	if nowW > 0 {
		head += "  " + cell("NOW", nowW)
	}
	if ageW > 0 {
		head += "  " + runewidth.FillLeft("AGE", ageW)
	}
	b.WriteString(wsDim.Render(head) + "\n")

	// Reserve space for the events panel that fills leftover screen; it
	// shrinks to nothing when the project list needs the room.
	visible := m.height - headerLines - 3
	if visible < 1 {
		visible = 1
	}
	offset := 0
	if m.cursor >= visible {
		offset = m.cursor - visible + 1
	}

	shownUnregisteredHeader := false
	linesUsed := 0
	for i := offset; i < len(m.rows) && linesUsed < visible; i++ {
		row := m.rows[i]
		if !row.registered && !shownUnregisteredHeader {
			shownUnregisteredHeader = true
			if linesUsed+2 <= visible {
				b.WriteString("\n" + wsDim.Render("  unregistered sessions:") + "\n")
				linesUsed += 2
			}
			if linesUsed >= visible {
				break
			}
		}
		b.WriteString(m.renderRow(row, i == m.cursor, nameW, stateW, branchW, srvW, nowW, ageW) + "\n")
		linesUsed++
		// Live tail under active rows: today's tokens, then recent activity.
		if len(row.tail) > 0 && linesUsed < visible {
			if row.tokIn > 0 || row.tokOut > 0 {
				tok := "    └ " + humanTokens(row.tokIn) + " in / " + humanTokens(row.tokOut) + " out today"
				b.WriteString(wsDim.Render(truncLine(tok, m.width)) + "\n")
				linesUsed++
			}
			for _, t := range row.tail {
				if linesUsed >= visible {
					break
				}
				b.WriteString(wsDim.Render(truncLine("      "+t, m.width)) + "\n")
				linesUsed++
			}
		}
	}
	if len(m.rows) == 0 {
		b.WriteString(wsDim.Render("  no projects registered — fleet add <path>") + "\n")
	}

	// Queue line: pending work waiting for agents to go idle.
	if len(m.queued) > 0 && linesUsed+1 < visible {
		parts := make([]string, 0, len(m.queued))
		for _, q := range m.queued {
			parts = append(parts, fmt.Sprintf("%s#%d %s", q.Project, q.ID, clip(q.Prompt, 30)))
		}
		b.WriteString(truncLine(wsState[event.StateWorking].Render(fmt.Sprintf("  QUEUE (%d)  ", len(m.queued)))+
			wsDim.Render(strings.Join(parts, " · ")), m.width) + "\n")
		linesUsed++
	}

	// Bottom panel fills whatever vertical space the table left over:
	// RESULTS (each agent's latest reply — the useful view) by default,
	// raw EVENTS behind the `e` toggle.
	if remaining := visible - linesUsed; remaining >= 3 {
		if m.showEvents {
			m.renderEvents(&b, remaining)
		} else {
			m.renderResults(&b, remaining)
		}
	}

	b.WriteString("\n")
	if m.flash.text != "" {
		style := wsOK
		if m.flash.isErr {
			style = wsErr
		}
		b.WriteString(truncLine("  "+style.Render(m.flash.text), m.width) + "\n")
	} else {
		b.WriteString("\n")
	}

	if m.dispatching {
		name := ""
		if row := m.selected(); row != nil {
			name = row.name
		}
		b.WriteString(truncLine("  dispatch → "+wsTitle.Render(name)+": "+m.input.View(), m.width))
	} else {
		b.WriteString(truncLine(wsDim.Render("  ↑/↓ move · enter jump · d dispatch · e results/events · r refresh · q quit"), m.width))
	}
	return b.String()
}

// renderResults shows each project's most recent agent reply — a glanceable
// "what has my fleet delivered lately" review.
func (m watchModel) renderResults(b *strings.Builder, budget int) {
	if len(m.results) == 0 {
		return
	}
	b.WriteString("\n" + wsDim.Render("  ── LAST RESULTS "+strings.Repeat("─", max(0, m.width-19))) + "\n")
	lines := 2
	for _, r := range m.results {
		if lines+2 > budget {
			break
		}
		b.WriteString("  " + wsOK.Render(r.Project) + wsDim.Render("  ·  "+humanAge(r.UpdatedAt)+" ago") + "\n")
		lines++
		snippet := strings.Join(strings.Fields(r.Snippet), " ") // collapse newlines
		for _, ln := range wrap(snippet, m.width-6, 2) {
			if lines >= budget {
				break
			}
			b.WriteString("    " + ln + "\n")
			lines++
		}
	}
}

// renderEvents is the raw hook-event feed (toggle: e). PostToolUse mirrors
// PreToolUse and SessionEnd is bookkeeping — both skipped.
func (m watchModel) renderEvents(b *strings.Builder, budget int) {
	feed := make([]store.EventRow, 0, len(m.events))
	for _, e := range m.events {
		if e.Event == event.PostToolUse || e.Event == event.SessionEnd {
			continue
		}
		feed = append(feed, e)
	}
	if len(feed) == 0 {
		return
	}
	b.WriteString("\n" + wsDim.Render("  ── EVENTS "+strings.Repeat("─", max(0, m.width-13))) + "\n")
	n := budget - 2
	if n > len(feed) {
		n = len(feed)
	}
	for i := n - 1; i >= 0; i-- { // oldest of the slice first
		e := feed[i]
		label := e.Event
		if e.Event == event.PermissionRequest {
			label = "⚠ PERMISSION"
		}
		line := fmt.Sprintf("  %s  %-22s %-14s %s",
			time.Unix(e.CreatedAt, 0).Format("15:04:05"), clip(e.Project, 22), label, e.Tool)
		if e.Summary != "" {
			line += ": " + e.Summary
		}
		style := wsDim
		if e.Event == event.PermissionRequest {
			style = wsErr
		}
		b.WriteString(style.Render(truncLine(line, m.width)) + "\n")
	}
}

// wrap splits s into at most maxLines lines of w display columns.
func wrap(s string, w, maxLines int) []string {
	if w < 10 {
		w = 10
	}
	var out []string
	for len(s) > 0 && len(out) < maxLines {
		if runewidth.StringWidth(s) <= w {
			out = append(out, s)
			break
		}
		cut := runewidth.Truncate(s, w, "")
		if i := strings.LastIndex(cut, " "); i > w/2 {
			cut = cut[:i]
		}
		if len(out) == maxLines-1 {
			out = append(out, runewidth.Truncate(s, w, "…"))
			break
		}
		out = append(out, cut)
		s = strings.TrimSpace(s[len(cut):])
	}
	return out
}

// truncLine truncates a possibly-styled line to w display columns.
func truncLine(s string, w int) string {
	if lipgloss.Width(s) <= w {
		return s
	}
	return ansi.Truncate(s, w, "…")
}
