package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const pomodoroLength = 25 * time.Minute

type view int

const (
	viewHistory view = iota
	viewDay
	viewReport
)

type mode int

const (
	modeNormal mode = iota
	modeAdd
	modeEdit
	modeConfirmDelete
)

type tickMsg time.Time

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// ---------- styles ----------

var (
	cAccent = lipgloss.AdaptiveColor{Light: "#5A3FD9", Dark: "#9D8CFF"}
	cGreen  = lipgloss.AdaptiveColor{Light: "#0A8F5A", Dark: "#3DDC97"}
	cYellow = lipgloss.AdaptiveColor{Light: "#B7791F", Dark: "#F6C85F"}
	cRed    = lipgloss.AdaptiveColor{Light: "#C53030", Dark: "#FF6B81"}
	cDim    = lipgloss.AdaptiveColor{Light: "#8A8A8A", Dark: "#6C6C7A"}
	cText   = lipgloss.AdaptiveColor{Light: "#1F1F28", Dark: "#E6E6F0"}
	cSelBg  = lipgloss.AdaptiveColor{Light: "#E8E4FF", Dark: "#2E2A4A"}

	sTitle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(cAccent).Padding(0, 1)
	sTab     = lipgloss.NewStyle().Foreground(cDim).Padding(0, 1)
	sTabOn   = lipgloss.NewStyle().Foreground(cAccent).Bold(true).Underline(true).Padding(0, 1)
	sDim     = lipgloss.NewStyle().Foreground(cDim)
	sText    = lipgloss.NewStyle().Foreground(cText)
	sBold    = lipgloss.NewStyle().Bold(true).Foreground(cText)
	sAccent  = lipgloss.NewStyle().Foreground(cAccent).Bold(true)
	sGreen   = lipgloss.NewStyle().Foreground(cGreen)
	sYellow  = lipgloss.NewStyle().Foreground(cYellow)
	sRed     = lipgloss.NewStyle().Foreground(cRed)
	sSel     = lipgloss.NewStyle().Background(cSelBg)
	sHeader  = lipgloss.NewStyle().Foreground(cDim).Bold(true)
	sBox     = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cDim).Padding(0, 1)
	sTimer   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cGreen).Padding(0, 1)
	sKey     = lipgloss.NewStyle().Foreground(cAccent).Bold(true)
	sSection = lipgloss.NewStyle().Foreground(cAccent).Bold(true).MarginTop(1)
)

// ---------- model ----------

type model struct {
	store *Store
	now   time.Time
	today string

	view   view
	mode   mode
	width  int
	height int

	day        string // day shown in the Day / Report views
	cursor     int    // selected task in the Day view
	histCursor int    // selected row in the History view
	histOffset int
	histDays   int

	input  textinput.Model
	editID int

	status    string
	statusErr bool
}

func newModel(s *Store) model {
	now := time.Now()
	ti := textinput.New()
	ti.Prompt = "› "
	ti.CharLimit = 200
	ti.PromptStyle = sAccent
	m := model{
		store:    s,
		now:      now,
		today:    dateKey(now),
		day:      dateKey(now),
		histDays: 14,
		input:    ti,
		width:    80,
		height:   24,
	}
	if a := s.Active; a != nil {
		if t := s.Find(a.TaskID); t != nil {
			m.setStatus(fmt.Sprintf("Timer still running on “%s”", t.Title))
		}
	} else if n := s.PendingBefore(m.today); n > 0 {
		m.setStatus(fmt.Sprintf("%d unfinished task(s) from previous days — press c in Today to bring them over", n))
	}
	return m
}

func (m model) Init() tea.Cmd { return tick() }

func (m *model) setStatus(s string) { m.status, m.statusErr = s, false }
func (m *model) setError(err error) { m.status, m.statusErr = "Error: "+err.Error(), true }

func (m *model) save() {
	if err := m.store.Save(); err != nil {
		m.setError(err)
	}
}

func (m model) dayTasks() []*Task { return m.store.TasksOn(m.day) }

func (m model) selectedTask() *Task {
	ts := m.dayTasks()
	if m.cursor < 0 || m.cursor >= len(ts) {
		return nil
	}
	return ts[m.cursor]
}

func (m *model) clampCursor() {
	n := len(m.dayTasks())
	if m.cursor >= n {
		m.cursor = n - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

// moveCursorTo keeps the selection on task `id` after the list re-sorts.
func (m *model) moveCursorTo(id int) {
	for i, t := range m.dayTasks() {
		if t.ID == id {
			m.cursor = i
			return
		}
	}
	m.clampCursor()
}

// ---------- update ----------

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.scrollHistory()
		return m, nil

	case tickMsg:
		m.now = time.Time(msg)
		if d := dateKey(m.now); d != m.today { // midnight passed
			if m.day == m.today {
				m.day = d
			}
			m.today = d
		}
		var cmd tea.Cmd
		if a := m.store.Active; a != nil && a.Target > 0 && m.now.Sub(a.Start) >= a.Target {
			title := "task"
			if t := m.store.Find(a.TaskID); t != nil {
				title = t.Title
			}
			m.store.StopTimer(m.now)
			m.save()
			m.setStatus(fmt.Sprintf("🍅 Pomodoro finished for “%s” — take a break!", title))
			cmd = notify("Pomodoro finished", title)
		}
		return m, tea.Batch(tick(), cmd)

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		switch m.mode {
		case modeAdd, modeEdit:
			return m.updateInput(msg)
		case modeConfirmDelete:
			return m.updateConfirm(msg)
		}
		return m.updateNormal(msg)
	}
	return m, nil
}

func (m model) updateInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeNormal
		m.input.Blur()
		return m, nil
	case "enter":
		title, cat := parseTaskInput(m.input.Value())
		if title == "" {
			m.setStatus("Title is empty — nothing saved")
		} else if m.mode == modeAdd {
			t := m.store.Add(title, cat, "", m.day, m.now)
			m.save()
			m.moveCursorTo(t.ID)
			m.setStatus("Added “" + title + "”")
		} else if t := m.store.Find(m.editID); t != nil {
			t.Title, t.Category = title, cat
			m.save()
			m.setStatus("Updated “" + title + "”")
		}
		m.mode = modeNormal
		m.input.Blur()
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m model) updateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "y" || msg.String() == "Y" {
		if t := m.store.Find(m.editID); t != nil {
			if err := m.store.Delete(t.ID); err != nil {
				m.setError(err)
			} else {
				m.save()
				m.setStatus("Deleted “" + t.Title + "”")
			}
		}
		m.clampCursor()
	} else {
		m.setStatus("Delete cancelled")
	}
	m.mode = modeNormal
	return m, nil
}

func (m model) updateNormal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// global keys
	switch key {
	case "q":
		return m, tea.Quit
	case "1":
		m.view = viewHistory
		return m, nil
	case "2", "t":
		m.view, m.day = viewDay, m.today
		m.clampCursor()
		return m, nil
	case "3":
		m.view = viewReport
		return m, nil
	case "tab":
		m.view = (m.view + 1) % 3
		m.clampCursor()
		return m, nil
	case "shift+tab":
		m.view = (m.view + 2) % 3
		m.clampCursor()
		return m, nil
	case "S": // stop the running timer from anywhere
		if m.store.Active != nil {
			d := m.store.StopTimer(m.now)
			m.save()
			m.setStatus("Timer stopped — " + fmtDur(d) + " recorded")
		}
		return m, nil
	}

	switch m.view {
	case viewHistory:
		return m.updateHistory(key)
	case viewDay:
		return m.updateDay(key)
	case viewReport:
		return m.updateReport(key)
	}
	return m, nil
}

// histRows is how many history rows fit on screen.
func (m model) histRows() int {
	avail := m.height - 14
	if m.store.Active != nil {
		avail -= 3
	}
	return max(avail, 3)
}

// scrollHistory keeps the selected history row visible.
func (m *model) scrollHistory() {
	avail := m.histRows()
	if m.histCursor < m.histOffset {
		m.histOffset = m.histCursor
	}
	if m.histCursor >= m.histOffset+avail {
		m.histOffset = m.histCursor - avail + 1
	}
	m.histOffset = max(0, min(m.histOffset, m.histDays-avail))
}

func (m model) updateHistory(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "up", "k":
		if m.histCursor > 0 {
			m.histCursor--
		}
	case "down", "j":
		if m.histCursor < m.histDays-1 {
			m.histCursor++
		}
	case "w": // cycle 7 / 14 / 30 days
		switch m.histDays {
		case 7:
			m.histDays = 14
		case 14:
			m.histDays = 30
		default:
			m.histDays = 7
		}
		if m.histCursor >= m.histDays {
			m.histCursor = m.histDays - 1
		}
	case "enter", "l", "right":
		m.day = shiftDate(m.today, -m.histCursor)
		m.view, m.cursor = viewDay, 0
	case "r":
		m.day = shiftDate(m.today, -m.histCursor)
		m.view = viewReport
	}
	m.scrollHistory()
	return m, nil
}

func (m model) updateDay(key string) (tea.Model, tea.Cmd) {
	t := m.selectedTask()
	switch key {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.dayTasks())-1 {
			m.cursor++
		}
	case "[", "h", "left":
		m.day, m.cursor = shiftDate(m.day, -1), 0
	case "]", "l", "right":
		m.day, m.cursor = shiftDate(m.day, 1), 0
	case "a", "n":
		m.mode, m.editID = modeAdd, 0
		m.input.SetValue("")
		m.input.Placeholder = "What do you want to do?  (add #category at the end)"
		return m, m.input.Focus()
	case "e":
		if t == nil {
			return m, nil
		}
		m.mode, m.editID = modeEdit, t.ID
		v := t.Title
		if t.Category != "" {
			v += " #" + t.Category
		}
		m.input.SetValue(v)
		m.input.CursorEnd()
		return m, m.input.Focus()
	case " ", "x":
		if t == nil {
			return m, nil
		}
		if err := m.store.SetDone(t.ID, !t.Done, m.now); err != nil {
			m.setError(err)
			return m, nil
		}
		m.save()
		if t.Done {
			m.setStatus("✔ Done: " + t.Title)
		} else {
			m.setStatus("Reopened: " + t.Title)
		}
		m.clampCursor()
	case "enter", "s", "p":
		if t == nil {
			return m, nil
		}
		if a := m.store.Active; a != nil && a.TaskID == t.ID {
			d := m.store.StopTimer(m.now)
			m.save()
			m.setStatus("Timer stopped — " + fmtDur(d) + " recorded on “" + t.Title + "”")
			return m, nil
		}
		var target time.Duration
		if key == "p" {
			target = pomodoroLength
		}
		if t.Done {
			_ = m.store.SetDone(t.ID, false, m.now)
		}
		if err := m.store.StartTimer(t.ID, target, m.now); err != nil {
			m.setError(err)
			return m, nil
		}
		m.save()
		m.moveCursorTo(t.ID)
		if target > 0 {
			m.setStatus("🍅 Pomodoro started (25m) on “" + t.Title + "”")
		} else {
			m.setStatus("⏱ Timer started on “" + t.Title + "”")
		}
	case "d", "delete", "backspace":
		if t == nil {
			return m, nil
		}
		m.mode, m.editID = modeConfirmDelete, t.ID
	case "c":
		if m.day != m.today {
			m.setStatus("Carry-over works from Today (press t)")
			return m, nil
		}
		n := m.store.CarryOver(m.today)
		if n > 0 {
			m.save()
		}
		m.setStatus(fmt.Sprintf("Brought %d unfinished task(s) into today", n))
	case "r":
		m.view = viewReport
	}
	return m, nil
}

func (m model) updateReport(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "[", "h", "left":
		m.day = shiftDate(m.day, -1)
	case "]", "l", "right":
		m.day = shiftDate(m.day, 1)
	case "x", "e":
		p, err := ExportReport(m.store, BuildReport(m.store, m.day, m.now))
		if err != nil {
			m.setError(err)
		} else {
			m.setStatus("Report saved to " + p)
		}
	case "enter":
		m.view, m.cursor = viewDay, 0
	}
	return m, nil
}

// parseTaskInput splits "Write report #work" into title and category.
func parseTaskInput(s string) (title, category string) {
	fields := strings.Fields(s)
	var keep []string
	for _, f := range fields {
		if strings.HasPrefix(f, "#") && len(f) > 1 && category == "" {
			category = strings.TrimPrefix(f, "#")
			continue
		}
		keep = append(keep, f)
	}
	return strings.Join(keep, " "), category
}

// notify rings the terminal bell and shows a desktop notification on macOS/Linux.
func notify(title, body string) tea.Cmd {
	return func() tea.Msg {
		fmt.Fprint(os.Stderr, "\a")
		switch runtime.GOOS {
		case "darwin":
			script := fmt.Sprintf("display notification %q with title %q sound name \"Glass\"", body, title)
			_ = exec.Command("osascript", "-e", script).Run()
		case "linux":
			_ = exec.Command("notify-send", title, body).Run()
		}
		return nil
	}
}

// ---------- view ----------

func (m model) View() string {
	var body string
	switch m.view {
	case viewHistory:
		body = m.viewHistory()
	case viewDay:
		body = m.viewDay()
	case viewReport:
		body = m.viewReport()
	}

	parts := []string{m.viewHeader()}
	if tb := m.viewTimerBox(); tb != "" {
		parts = append(parts, tb)
	}
	parts = append(parts, body, m.viewFooter())
	return lipgloss.NewStyle().Padding(0, 1).Render(lipgloss.JoinVertical(lipgloss.Left, parts...))
}

func (m model) viewHeader() string {
	title := sTitle.Render("✓ Task Manager")
	tabs := []string{"History", "Today", "Report"}
	var rendered []string
	for i, name := range tabs {
		label := fmt.Sprintf("%d %s", i+1, name)
		if view(i) == m.view {
			rendered = append(rendered, sTabOn.Render(label))
		} else {
			rendered = append(rendered, sTab.Render(label))
		}
	}
	left := lipgloss.JoinHorizontal(lipgloss.Top, title, "  ", strings.Join(rendered, ""))
	right := sDim.Render(m.now.Format("Mon 02 Jan 2006  15:04"))
	gap := m.width - 2 - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 2 {
		return left + "\n"
	}
	return left + strings.Repeat(" ", gap) + right + "\n"
}

func (m model) viewTimerBox() string {
	a := m.store.Active
	if a == nil {
		return ""
	}
	t := m.store.Find(a.TaskID)
	if t == nil {
		return ""
	}
	elapsed := m.store.Elapsed(m.now)
	var line string
	if a.Target > 0 {
		left := a.Target - elapsed
		bar := progressBar(float64(elapsed)/float64(a.Target), 20, cRed)
		line = fmt.Sprintf("%s  %s  %s  %s",
			sRed.Bold(true).Render("🍅 "+fmtClock(left)),
			bar,
			sBold.Render(truncate(t.Title, 40)),
			sDim.Render("of 25:00"))
	} else {
		line = fmt.Sprintf("%s  %s  %s",
			sGreen.Bold(true).Render("⏱  "+fmtClock(elapsed)),
			sBold.Render(truncate(t.Title, 50)),
			sDim.Render("total "+fmtDur(m.store.TaskTime(t, m.now))))
	}
	line += sDim.Render("   (S to stop)")
	return sTimer.Render(line)
}

func (m model) viewFooter() string {
	var help string
	switch {
	case m.mode == modeAdd || m.mode == modeEdit:
		help = keys("enter", "save", "esc", "cancel")
	case m.mode == modeConfirmDelete:
		help = keys("y", "confirm delete", "any key", "cancel")
	case m.view == viewHistory:
		help = keys("↑↓", "select day", "enter", "open day", "r", "report", "w", "7/14/30 days", "t", "today", "q", "quit")
	case m.view == viewDay:
		help = keys("a", "add", "space", "done", "enter", "timer", "p", "pomodoro", "e", "edit", "d", "delete",
			"[ ]", "prev/next day", "c", "carry over", "r", "report", "q", "quit")
	case m.view == viewReport:
		help = keys("[ ]", "prev/next day", "x", "export .md", "enter", "open day", "tab", "switch view", "q", "quit")
	}
	status := ""
	if m.status != "" {
		if m.statusErr {
			status = sRed.Render(m.status)
		} else {
			status = sYellow.Render(m.status)
		}
	}
	return "\n" + status + "\n" + lipgloss.NewStyle().Width(max(20, m.width-2)).Render(help)
}

func keys(pairs ...string) string {
	var out []string
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, sKey.Render(pairs[i])+" "+sDim.Render(pairs[i+1]))
	}
	return strings.Join(out, sDim.Render("  ·  "))
}

// ----- History -----

func (m model) viewHistory() string {
	hist := m.store.History(m.today, m.histDays, m.now)

	barW := 16
	if m.width < 80 {
		barW = 8
	}
	header := sHeader.Render(fmt.Sprintf("  %-12s %-4s %-7s %-*s %8s", "Date", "Day", "Done", barW+6, "Progress", "Focus"))

	avail := m.histRows()
	end := min(len(hist), m.histOffset+avail)

	var maxFocus time.Duration
	for _, h := range hist {
		maxFocus = max(maxFocus, h.Focus)
	}

	rows := []string{header}
	for i := m.histOffset; i < end; i++ {
		h := hist[i]
		t, _ := parseDate(h.Date)
		dayName := t.Format("Mon")
		dateStr := h.Date
		if h.Date == m.today {
			dateStr += " •"
		}

		done := sDim.Render(fmt.Sprintf("%-7s", "—"))
		bar := sDim.Render(strings.Repeat("·", barW)) + "      "
		if h.Total > 0 {
			pct := float64(h.Done) / float64(h.Total)
			done = sText.Render(fmt.Sprintf("%-7s", fmt.Sprintf("%d/%d", h.Done, h.Total)))
			bar = progressBar(pct, barW, pctColor(pct)) + fmt.Sprintf(" %4d%%", int(pct*100))
		}
		focus := sDim.Render(fmt.Sprintf("%8s", "—"))
		if h.Focus > 0 {
			focus = sText.Render(fmt.Sprintf("%8s", fmtDur(h.Focus))) + " " + focusSpark(h.Focus, maxFocus)
		}

		pointer := "  "
		if i == m.histCursor {
			pointer = sAccent.Render("▸ ")
		}
		row := pointer + sBold.Render(fmt.Sprintf("%-12s", dateStr)) + " " + sText.Render(fmt.Sprintf("%-4s", dayName)) + " " + done + " " + bar + " " + focus
		if i == m.histCursor {
			row = sSel.Render(padRight(row, m.width-8))
		}
		rows = append(rows, row)
	}

	// totals
	var tDone, tTotal, activeDays int
	var tFocus time.Duration
	for _, h := range hist {
		tDone += h.Done
		tTotal += h.Total
		tFocus += h.Focus
		if h.Done > 0 || h.Focus > 0 {
			activeDays++
		}
	}
	avg := time.Duration(0)
	if activeDays > 0 {
		avg = tFocus / time.Duration(activeDays)
	}
	streak := m.store.Streak(m.today, m.now)
	stats := strings.Join([]string{
		stat("🔥 Streak", fmt.Sprintf("%d day(s)", streak)),
		stat("✔ Done", fmt.Sprintf("%d/%d", tDone, tTotal)),
		stat("⏱ Focus", fmtDur(tFocus)),
		stat("⌀ per active day", fmtDur(avg)),
	}, "    ")

	title := sAccent.Render(fmt.Sprintf("Last %d days", m.histDays))
	return title + "\n" + sBox.Render(strings.Join(rows, "\n")) + "\n" + stats
}

func stat(label, value string) string {
	return sDim.Render(label+" ") + sBold.Render(value)
}

// ----- Day -----

func (m model) dayTitle() string {
	t, _ := parseDate(m.day)
	label := t.Format("Monday, 02 January 2006")
	switch m.day {
	case m.today:
		label += "  " + sGreen.Render("(today)")
	case shiftDate(m.today, -1):
		label += "  " + sDim.Render("(yesterday)")
	case shiftDate(m.today, 1):
		label += "  " + sDim.Render("(tomorrow)")
	}
	return sDim.Render("‹ ") + sAccent.Render(label) + sDim.Render(" ›")
}

func (m model) viewDay() string {
	tasks := m.dayTasks()
	var b strings.Builder
	b.WriteString(m.dayTitle() + "\n\n")

	titleW := max(20, m.width-40)

	if len(tasks) == 0 {
		b.WriteString(sDim.Render("  No tasks for this day yet. Press ") + sKey.Render("a") + sDim.Render(" to add one.") + "\n")
	}
	for i, t := range tasks {
		check := sDim.Render("[ ]")
		title := sText.Render(truncate(t.Title, titleW))
		if t.Done {
			check = sGreen.Render("[✓]")
			title = sDim.Strikethrough(true).Render(truncate(t.Title, titleW))
		}
		cat := ""
		if t.Category != "" {
			cat = sYellow.Render("#" + t.Category)
		}
		spent := m.store.TaskTime(t, m.now)
		timeStr := ""
		if spent > 0 {
			timeStr = sDim.Render(fmtDur(spent))
		}
		running := "  "
		if a := m.store.Active; a != nil && a.TaskID == t.ID {
			running = sGreen.Render("● ")
			if a.Target > 0 {
				running = sRed.Render("● ")
			}
			timeStr = sGreen.Render(fmtClock(m.store.Elapsed(m.now)))
		}
		pointer := "  "
		if i == m.cursor && m.mode != modeAdd {
			pointer = sAccent.Render("▸ ")
		}
		row := pointer + check + " " + running + padRight(title, titleW) + "  " + padRight(cat, 14) + " " + timeStr
		if i == m.cursor && m.mode != modeAdd {
			row = sSel.Render(padRight(row, m.width-4))
		}
		b.WriteString(row + "\n")
	}

	switch m.mode {
	case modeAdd:
		b.WriteString("\n" + sAccent.Render("New task") + "\n" + m.input.View() + "\n")
	case modeEdit:
		b.WriteString("\n" + sAccent.Render("Edit task") + "\n" + m.input.View() + "\n")
	case modeConfirmDelete:
		if t := m.store.Find(m.editID); t != nil {
			b.WriteString("\n" + sRed.Render("Delete “"+t.Title+"”? ") + sKey.Render("y") + sDim.Render("/n") + "\n")
		}
	}

	sum := m.store.Summary(m.day, m.now)
	pct := 0.0
	if sum.Total > 0 {
		pct = float64(sum.Done) / float64(sum.Total)
	}
	b.WriteString("\n" + progressBar(pct, 24, pctColor(pct)) + "  " +
		stat("done", fmt.Sprintf("%d/%d", sum.Done, sum.Total)) + "    " +
		stat("focus", fmtDur(sum.Focus)))
	if m.day == m.today {
		if n := m.store.PendingBefore(m.today); n > 0 {
			b.WriteString("    " + sYellow.Render(fmt.Sprintf("%d left from earlier days (c)", n)))
		}
	}
	return b.String()
}

// ----- Report -----

func (m model) viewReport() string {
	r := BuildReport(m.store, m.day, m.now)
	var b strings.Builder
	t, _ := parseDate(m.day)
	b.WriteString(sDim.Render("‹ ") + sAccent.Render("Daily report · "+t.Format("Monday, 02 January 2006")) + sDim.Render(" ›") + "\n")

	if r.Total == 0 && r.Focus == 0 {
		b.WriteString("\n" + sDim.Render("  Nothing recorded for this day.") + "\n")
		return b.String()
	}

	pct := float64(r.Percent()) / 100
	cards := []string{
		card("Completed", fmt.Sprintf("%d / %d", r.Done, r.Total), fmt.Sprintf("%d%%", r.Percent()), pctColor(pct)),
		card("Focus time", fmtDur(r.Focus), fmt.Sprintf("%d session(s)", r.Sessions), cGreen),
	}
	if !r.FirstStart.IsZero() {
		cards = append(cards, card("Worked", r.FirstStart.Local().Format("15:04")+" – "+r.LastEnd.Local().Format("15:04"),
			"first → last", cAccent))
	}
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, cards...) + "\n")

	if len(r.Categories) > 0 {
		b.WriteString(sSection.Render("By category") + "\n")
		var maxT time.Duration
		for _, c := range r.Categories {
			maxT = max(maxT, c.Time)
		}
		for _, c := range r.Categories {
			frac := 0.0
			if maxT > 0 {
				frac = float64(c.Time) / float64(maxT)
			}
			b.WriteString(fmt.Sprintf("  %s %s %s  %s\n",
				padRight(sYellow.Render("#"+truncate(c.Name, 16)), 18),
				progressBar(frac, 20, cAccent),
				sText.Render(fmt.Sprintf("%8s", fmtDur(c.Time))),
				sDim.Render(fmt.Sprintf("%d/%d done", c.Done, c.Total))))
		}
	}

	lines := func(title string, ls []TaskLine, mark string, st lipgloss.Style) {
		if len(ls) == 0 {
			return
		}
		b.WriteString(sSection.Render(title) + "\n")
		for _, l := range ls {
			tm := ""
			if l.Time > 0 {
				tm = sDim.Render(fmtDur(l.Time))
			}
			cat := ""
			if l.Task.Category != "" {
				cat = sYellow.Render("#" + l.Task.Category)
			}
			b.WriteString("  " + st.Render(mark) + " " + padRight(sText.Render(truncate(l.Task.Title, 40)), 42) + padRight(cat, 14) + " " + tm + "\n")
		}
	}
	lines("Done", r.DoneTasks, "✓", sGreen)
	lines("Not done", r.OpenTasks, "○", sRed)
	lines("Also worked on (other days' tasks)", r.OtherTasks, "·", sDim)
	return b.String()
}

func card(label, value, sub string, color lipgloss.TerminalColor) string {
	st := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(color).Padding(0, 2).MarginRight(1)
	return st.Render(sDim.Render(label) + "\n" +
		lipgloss.NewStyle().Bold(true).Foreground(color).Render(value) + "\n" +
		sDim.Render(sub))
}

// ---------- helpers ----------

func progressBar(frac float64, width int, color lipgloss.TerminalColor) string {
	frac = min(max(frac, 0), 1)
	filled := int(frac*float64(width) + 0.5)
	return lipgloss.NewStyle().Foreground(color).Render(strings.Repeat("█", filled)) +
		sDim.Render(strings.Repeat("░", width-filled))
}

func pctColor(p float64) lipgloss.TerminalColor {
	switch {
	case p >= 0.75:
		return cGreen
	case p >= 0.4:
		return cYellow
	default:
		return cRed
	}
}

// focusSpark draws a tiny bar relative to the best day in the table.
func focusSpark(d, maxD time.Duration) string {
	if maxD <= 0 {
		return ""
	}
	blocks := []rune("▁▂▃▄▅▆▇█")
	i := int(float64(d) / float64(maxD) * float64(len(blocks)-1))
	return sAccent.Render(string(blocks[i]))
}

func truncate(s string, w int) string { return ansi.Truncate(s, w, "…") }

func padRight(s string, w int) string {
	if gap := w - lipgloss.Width(s); gap > 0 {
		return s + strings.Repeat(" ", gap)
	}
	return s
}

func runTUI(s *Store) error {
	p := tea.NewProgram(newModel(s), tea.WithAltScreen())
	_, err := p.Run()
	return err
}
