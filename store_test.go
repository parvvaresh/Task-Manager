package main

import (
	"path/filepath"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := LoadStore(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func at(day, clock string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04", day+" "+clock, time.Local)
	if err != nil {
		panic(err)
	}
	return t
}

func TestTimerAndSummary(t *testing.T) {
	s := newTestStore(t)
	day := "2026-11-05"
	a := s.Add("Write report", "work", "", day, at(day, "08:00"))
	b := s.Add("Read", "", "", day, at(day, "08:00"))

	if err := s.StartTimer(a.ID, 0, at(day, "09:00")); err != nil {
		t.Fatal(err)
	}
	// starting another timer stops the first one
	if err := s.StartTimer(b.ID, 0, at(day, "10:00")); err != nil {
		t.Fatal(err)
	}
	if got := s.TaskTime(a, at(day, "11:00")); got != time.Hour {
		t.Fatalf("task a time = %v, want 1h", got)
	}
	// finishing a task stops its running timer
	if err := s.SetDone(b.ID, true, at(day, "10:30")); err != nil {
		t.Fatal(err)
	}
	if s.Active != nil {
		t.Fatal("timer should be stopped after marking done")
	}

	sum := s.Summary(day, at(day, "12:00"))
	if sum.Total != 2 || sum.Done != 1 || sum.Focus != 90*time.Minute {
		t.Fatalf("summary = %+v", sum)
	}

	r := BuildReport(s, day, at(day, "12:00"))
	if r.Percent() != 50 || r.Sessions != 2 || len(r.Categories) != 2 {
		t.Fatalf("report = %+v", r)
	}
}

func TestPomodoroIsCapped(t *testing.T) {
	s := newTestStore(t)
	day := "2026-11-05"
	a := s.Add("Focus", "", "", day, at(day, "08:00"))
	_ = s.StartTimer(a.ID, 25*time.Minute, at(day, "09:00"))
	if d := s.StopTimer(at(day, "10:00")); d != 25*time.Minute {
		t.Fatalf("pomodoro recorded %v, want 25m", d)
	}
}

func TestCarryOverAndStreak(t *testing.T) {
	s := newTestStore(t)
	today := "2026-11-06"
	y := shiftDate(today, -1)
	old := s.Add("old", "", "", shiftDate(today, -3), at(y, "08:00"))
	done := s.Add("done yesterday", "", "", y, at(y, "08:00"))
	_ = s.SetDone(done.ID, true, at(y, "09:00"))

	if n := s.Streak(today, at(today, "08:00")); n != 1 {
		t.Fatalf("streak = %d, want 1", n)
	}
	if n := s.CarryOver(today); n != 1 || old.Date != today {
		t.Fatalf("carry over moved %d, date %s", n, old.Date)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	s := newTestStore(t)
	s.Add("one", "", "", "2026-11-06", time.Now())
	_ = s.StartTimer(1, 0, time.Now())
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	s2, err := LoadStore(s.path)
	if err != nil {
		t.Fatal(err)
	}
	if len(s2.Tasks) != 1 || s2.Active == nil || s2.NextID != 2 {
		t.Fatalf("loaded store = %+v", s2)
	}
}

func TestParseTaskInput(t *testing.T) {
	title, cat := parseTaskInput("  Write the report #work  ")
	if title != "Write the report" || cat != "work" {
		t.Fatalf("got %q %q", title, cat)
	}
}
