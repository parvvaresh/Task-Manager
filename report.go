package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// CategoryStat is the per-category breakdown inside a daily report.
type CategoryStat struct {
	Name  string
	Total int
	Done  int
	Time  time.Duration
}

// TaskLine is one task inside a daily report.
type TaskLine struct {
	Task *Task
	Time time.Duration // time spent on this task during the report day
}

// Report is everything we know about one day.
type Report struct {
	Date       string
	Total      int
	Done       int
	Focus      time.Duration
	Sessions   int
	FirstStart time.Time
	LastEnd    time.Time
	Categories []CategoryStat
	DoneTasks  []TaskLine
	OpenTasks  []TaskLine
	// Tasks of other days that got tracked time on this day.
	OtherTasks []TaskLine
}

// Percent returns the completion rate (0-100).
func (r Report) Percent() int {
	if r.Total == 0 {
		return 0
	}
	return r.Done * 100 / r.Total
}

// BuildReport collects the daily report for `day`.
func BuildReport(s *Store, day string, now time.Time) Report {
	r := Report{Date: day}
	cats := map[string]*CategoryStat{}
	cat := func(name string) *CategoryStat {
		if name == "" {
			name = "uncategorized"
		}
		c, ok := cats[name]
		if !ok {
			c = &CategoryStat{Name: name}
			cats[name] = c
		}
		return c
	}

	addSpan := func(start, end time.Time) {
		r.Sessions++
		if r.FirstStart.IsZero() || start.Before(r.FirstStart) {
			r.FirstStart = start
		}
		if end.After(r.LastEnd) {
			r.LastEnd = end
		}
	}

	for _, t := range s.Tasks {
		spent := s.TaskTimeOn(t, day, now)
		for _, se := range t.Sessions {
			if dateKey(se.Start) == day {
				addSpan(se.Start, se.End)
			}
		}
		if s.Active != nil && s.Active.TaskID == t.ID && dateKey(s.Active.Start) == day {
			addSpan(s.Active.Start, now)
		}

		if t.Date == day {
			c := cat(t.Category)
			c.Total++
			c.Time += spent
			r.Total++
			if t.Done {
				c.Done++
				r.Done++
				r.DoneTasks = append(r.DoneTasks, TaskLine{t, spent})
			} else {
				r.OpenTasks = append(r.OpenTasks, TaskLine{t, spent})
			}
		} else if spent > 0 {
			cat(t.Category).Time += spent
			r.OtherTasks = append(r.OtherTasks, TaskLine{t, spent})
		}
		r.Focus += spent
	}

	for _, c := range cats {
		r.Categories = append(r.Categories, *c)
	}
	sort.Slice(r.Categories, func(i, j int) bool {
		if r.Categories[i].Time != r.Categories[j].Time {
			return r.Categories[i].Time > r.Categories[j].Time
		}
		return r.Categories[i].Name < r.Categories[j].Name
	})
	byTime := func(l []TaskLine) {
		sort.SliceStable(l, func(i, j int) bool { return l[i].Time > l[j].Time })
	}
	byTime(r.DoneTasks)
	byTime(r.OpenTasks)
	byTime(r.OtherTasks)
	return r
}

// Markdown renders the report as Markdown (used by the CLI and for export).
func (r Report) Markdown() string {
	var b strings.Builder
	t, _ := parseDate(r.Date)
	fmt.Fprintf(&b, "# Daily report — %s (%s)\n\n", r.Date, t.Format("Monday"))
	fmt.Fprintf(&b, "- **Completed:** %d / %d (%d%%)\n", r.Done, r.Total, r.Percent())
	fmt.Fprintf(&b, "- **Focus time:** %s in %d session(s)\n", fmtDur(r.Focus), r.Sessions)
	if !r.FirstStart.IsZero() {
		fmt.Fprintf(&b, "- **Worked between:** %s – %s\n",
			r.FirstStart.Local().Format("15:04"), r.LastEnd.Local().Format("15:04"))
	}
	if len(r.Categories) > 0 {
		b.WriteString("\n## By category\n\n| Category | Done | Time |\n|---|---|---|\n")
		for _, c := range r.Categories {
			fmt.Fprintf(&b, "| %s | %d/%d | %s |\n", c.Name, c.Done, c.Total, fmtDur(c.Time))
		}
	}
	section := func(title, mark string, lines []TaskLine) {
		if len(lines) == 0 {
			return
		}
		fmt.Fprintf(&b, "\n## %s\n\n", title)
		for _, l := range lines {
			fmt.Fprintf(&b, "- %s %s", mark, l.Task.Title)
			if l.Task.Category != "" {
				fmt.Fprintf(&b, " `#%s`", l.Task.Category)
			}
			if l.Time > 0 {
				fmt.Fprintf(&b, " — %s", fmtDur(l.Time))
			}
			b.WriteString("\n")
		}
	}
	section("Done", "[x]", r.DoneTasks)
	section("Not done", "[ ]", r.OpenTasks)
	section("Also worked on", "·", r.OtherTasks)
	if r.Total == 0 && r.Focus == 0 {
		b.WriteString("\n_Nothing recorded for this day._\n")
	}
	return b.String()
}

// ExportReport writes the Markdown report next to the data file and returns its path.
func ExportReport(s *Store, r Report) (string, error) {
	dir := filepath.Join(filepath.Dir(s.path), "reports")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	p := filepath.Join(dir, r.Date+".md")
	return p, os.WriteFile(p, []byte(r.Markdown()), 0o644)
}

// fmtDur formats a duration as "1h 05m", "12m" or "40s".
func fmtDur(d time.Duration) string {
	if d <= 0 {
		return "0m"
	}
	d = d.Round(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	switch {
	case h > 0:
		return fmt.Sprintf("%dh %02dm", h, m)
	case m > 0:
		return fmt.Sprintf("%dm", m)
	default:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
}

// fmtClock formats a duration as HH:MM:SS.
func fmtClock(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Round(time.Second)
	return fmt.Sprintf("%02d:%02d:%02d", int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60)
}
