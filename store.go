package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const dateLayout = "2006-01-02"

// Session is one continuous block of time spent on a task.
type Session struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// Task is a single to-do item planned for a specific day.
type Task struct {
	ID        int        `json:"id"`
	Title     string     `json:"title"`
	Category  string     `json:"category,omitempty"`
	Deadline  string     `json:"deadline,omitempty"`
	Date      string     `json:"date"` // day the task is planned for (YYYY-MM-DD)
	Done      bool       `json:"done"`
	DoneAt    *time.Time `json:"done_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	Sessions  []Session  `json:"sessions,omitempty"`
}

// ActiveTimer is the currently running timer. It is persisted, so it keeps
// running even after the app is closed.
type ActiveTimer struct {
	TaskID int           `json:"task_id"`
	Start  time.Time     `json:"start"`
	Target time.Duration `json:"target,omitempty"` // >0 means pomodoro / countdown
}

// Store holds all data and knows how to persist it.
type Store struct {
	path   string
	NextID int          `json:"next_id"`
	Tasks  []*Task      `json:"tasks"`
	Active *ActiveTimer `json:"active,omitempty"`
}

// DaySummary aggregates one day for the history table.
type DaySummary struct {
	Date  string
	Total int
	Done  int
	Focus time.Duration
}

func dateKey(t time.Time) string { return t.Local().Format(dateLayout) }

func parseDate(s string) (time.Time, error) {
	return time.ParseInLocation(dateLayout, s, time.Local)
}

func shiftDate(day string, delta int) string {
	t, err := parseDate(day)
	if err != nil {
		return day
	}
	return t.AddDate(0, 0, delta).Format(dateLayout)
}

// defaultDataPath returns $TASKMANAGER_DATA or ~/.taskmanager/data.json.
func defaultDataPath() string {
	if p := os.Getenv("TASKMANAGER_DATA"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "taskmanager.json"
	}
	return filepath.Join(home, ".taskmanager", "data.json")
}

// LoadStore reads the store from disk (an empty store if the file is missing).
func LoadStore(path string) (*Store, error) {
	s := &Store{path: path, NextID: 1}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, s); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	for _, t := range s.Tasks {
		if t.ID >= s.NextID {
			s.NextID = t.ID + 1
		}
	}
	return s, nil
}

// Save writes the store atomically.
func (s *Store) Save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Add creates a new task for the given day.
func (s *Store) Add(title, category, deadline, day string, now time.Time) *Task {
	t := &Task{
		ID:        s.NextID,
		Title:     title,
		Category:  category,
		Deadline:  deadline,
		Date:      day,
		CreatedAt: now,
	}
	s.NextID++
	s.Tasks = append(s.Tasks, t)
	return t
}

// Find returns the task with the given ID or nil.
func (s *Store) Find(id int) *Task {
	for _, t := range s.Tasks {
		if t.ID == id {
			return t
		}
	}
	return nil
}

// SetDone marks a task done/undone. Finishing a task stops its timer.
func (s *Store) SetDone(id int, done bool, now time.Time) error {
	t := s.Find(id)
	if t == nil {
		return fmt.Errorf("task %d not found", id)
	}
	if done && s.Active != nil && s.Active.TaskID == id {
		s.StopTimer(now)
	}
	t.Done = done
	if done {
		n := now
		t.DoneAt = &n
	} else {
		t.DoneAt = nil
	}
	return nil
}

// Delete removes a task (and its running timer, if any).
func (s *Store) Delete(id int) error {
	for i, t := range s.Tasks {
		if t.ID == id {
			if s.Active != nil && s.Active.TaskID == id {
				s.Active = nil
			}
			s.Tasks = append(s.Tasks[:i], s.Tasks[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("task %d not found", id)
}

// StartTimer starts a timer on a task, stopping any other running timer.
func (s *Store) StartTimer(id int, target time.Duration, now time.Time) error {
	if s.Find(id) == nil {
		return fmt.Errorf("task %d not found", id)
	}
	s.StopTimer(now)
	s.Active = &ActiveTimer{TaskID: id, Start: now, Target: target}
	return nil
}

// StopTimer stops the running timer and records the session.
// It returns the recorded duration (0 if nothing was running).
func (s *Store) StopTimer(now time.Time) time.Duration {
	if s.Active == nil {
		return 0
	}
	a := s.Active
	s.Active = nil
	end := now
	if a.Target > 0 && end.Sub(a.Start) > a.Target {
		end = a.Start.Add(a.Target) // a pomodoro never counts more than its length
	}
	d := end.Sub(a.Start)
	if d < time.Second {
		return 0
	}
	if t := s.Find(a.TaskID); t != nil {
		t.Sessions = append(t.Sessions, Session{Start: a.Start, End: end})
	}
	return d
}

// Elapsed returns how long the active timer has been running.
func (s *Store) Elapsed(now time.Time) time.Duration {
	if s.Active == nil {
		return 0
	}
	return now.Sub(s.Active.Start)
}

// TaskTime is the total time spent on a task (including the running timer).
func (s *Store) TaskTime(t *Task, now time.Time) time.Duration {
	var d time.Duration
	for _, se := range t.Sessions {
		d += se.End.Sub(se.Start)
	}
	if s.Active != nil && s.Active.TaskID == t.ID {
		d += now.Sub(s.Active.Start)
	}
	return d
}

// TaskTimeOn is the time spent on a task during the given day.
func (s *Store) TaskTimeOn(t *Task, day string, now time.Time) time.Duration {
	var d time.Duration
	for _, se := range t.Sessions {
		if dateKey(se.Start) == day {
			d += se.End.Sub(se.Start)
		}
	}
	if s.Active != nil && s.Active.TaskID == t.ID && dateKey(s.Active.Start) == day {
		d += now.Sub(s.Active.Start)
	}
	return d
}

// TasksOn returns the tasks planned for a day, unfinished first, in creation order.
func (s *Store) TasksOn(day string) []*Task {
	var out []*Task
	for _, t := range s.Tasks {
		if t.Date == day {
			out = append(out, t)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Done != out[j].Done {
			return !out[i].Done
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// FocusOn is the total tracked time whose sessions started on the given day.
func (s *Store) FocusOn(day string, now time.Time) time.Duration {
	var d time.Duration
	for _, t := range s.Tasks {
		d += s.TaskTimeOn(t, day, now)
	}
	return d
}

// Summary returns the aggregate numbers for one day.
func (s *Store) Summary(day string, now time.Time) DaySummary {
	sum := DaySummary{Date: day, Focus: s.FocusOn(day, now)}
	for _, t := range s.Tasks {
		if t.Date != day {
			continue
		}
		sum.Total++
		if t.Done {
			sum.Done++
		}
	}
	return sum
}

// History returns summaries for `days` days ending at `today`, newest first.
func (s *Store) History(today string, days int, now time.Time) []DaySummary {
	out := make([]DaySummary, 0, days)
	for i := 0; i < days; i++ {
		out = append(out, s.Summary(shiftDate(today, -i), now))
	}
	return out
}

// Streak counts consecutive days (ending today, or yesterday if nothing is
// done yet today) with at least one finished task.
func (s *Store) Streak(today string, now time.Time) int {
	day := today
	if s.Summary(day, now).Done == 0 {
		day = shiftDate(day, -1)
	}
	n := 0
	for s.Summary(day, now).Done > 0 {
		n++
		day = shiftDate(day, -1)
	}
	return n
}

// CarryOver moves all unfinished tasks from earlier days to `today`.
func (s *Store) CarryOver(today string) int {
	n := 0
	for _, t := range s.Tasks {
		if !t.Done && t.Date < today {
			t.Date = today
			n++
		}
	}
	return n
}

// PendingBefore counts unfinished tasks planned before `today`.
func (s *Store) PendingBefore(today string) int {
	n := 0
	for _, t := range s.Tasks {
		if !t.Done && t.Date < today {
			n++
		}
	}
	return n
}
