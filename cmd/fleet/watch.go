package main

import (
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
}

type (
	tickMsg time.Time
	dataMsg struct {
		rows          []watchRow
		tokIn, tokOut int64 // fleet-wide today
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
		// Best-effort extras: activity tails and today's token usage.
		events, _ := c.Events(80)
		usage, _ := c.Costs(1)
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
		}
		return dataMsg{rows: rows, tokIn: totIn, tokOut: totOut}
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
// on narrow terminals NAME shrinks first, then AGE and NOW drop entirely.
func (m watchModel) layout() (nameW, stateW, nowW, ageW int) {
	nameW, stateW, ageW = 24, 10, 5
	// indent(2) + icon(2) + gaps(2+2+2)
	nowW = m.width - nameW - stateW - ageW - 10
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
	return nameW, stateW, nowW, ageW
}

func (m watchModel) renderRow(row watchRow, selected bool, nameW, stateW, nowW, ageW int) string {
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

func (m watchModel) View() string {
	if m.width == 0 {
		return ""
	}
	nameW, stateW, nowW, ageW := m.layout()

	var b strings.Builder
	header := wsTitle.Render("FLEET") + "  " + wsDim.Render(time.Now().Format("15:04:05"))
	if m.tokIn > 0 || m.tokOut > 0 {
		header += "  " + wsDim.Render("· today "+humanTokens(m.tokIn)+" in / "+humanTokens(m.tokOut)+" out")
	}
	if m.fetchErr != nil {
		header += "  " + wsErr.Render("⚠ "+m.fetchErr.Error())
	}
	b.WriteString(truncLine(header, m.width) + "\n\n")

	head := "  " + cell("", 2) + cell("PROJECT", nameW) + "  " + cell("STATE", stateW)
	if nowW > 0 {
		head += "  " + cell("NOW", nowW)
	}
	if ageW > 0 {
		head += "  " + runewidth.FillLeft("AGE", ageW)
	}
	b.WriteString(wsDim.Render(head) + "\n")

	// Scroll window: keep the cursor visible in the space between the
	// 3 header lines and the 3 footer lines.
	visible := m.height - 6
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
		b.WriteString(m.renderRow(row, i == m.cursor, nameW, stateW, nowW, ageW) + "\n")
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
		b.WriteString(truncLine(wsDim.Render("  ↑/↓ move · enter jump · d dispatch · r refresh · q quit"), m.width))
	}
	return b.String()
}

// truncLine truncates a possibly-styled line to w display columns.
func truncLine(s string, w int) string {
	if lipgloss.Width(s) <= w {
		return s
	}
	return ansi.Truncate(s, w, "…")
}
