package main

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/mattn/go-isatty"
)

const usage = `Task Manager — daily tasks, timers and reports in your terminal.

Usage:
  taskmanager                          open the interactive dashboard
  taskmanager add <title> [deadline] [category]
                                       add a task for today
  taskmanager list [YYYY-MM-DD]        list tasks (default: today)
  taskmanager done <id>                mark a task as done
  taskmanager delete <id>              delete a task
  taskmanager start <id>               start a timer on a task
  taskmanager stop                     stop the running timer
  taskmanager report [YYYY-MM-DD]      print the daily report (Markdown)
  taskmanager history [days]           print the history table

Data is stored in ~/.taskmanager/data.json (override with TASKMANAGER_DATA).
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	s, err := LoadStore(defaultDataPath())
	if err != nil {
		return err
	}
	now := time.Now()
	today := dateKey(now)

	if len(args) == 0 {
		if !isatty.IsTerminal(os.Stdout.Fd()) {
			fmt.Print(usage)
			return nil
		}
		if err := runTUI(s); err != nil {
			return err
		}
		if s.Active != nil {
			if t := s.Find(s.Active.TaskID); t != nil {
				fmt.Printf("⏱  Timer still running on “%s” (%s). Run `taskmanager stop` or reopen to stop it.\n",
					t.Title, fmtClock(s.Elapsed(time.Now())))
			}
		}
		return nil
	}

	switch args[0] {
	case "add":
		if len(args) < 2 {
			return fmt.Errorf("usage: taskmanager add <title> [deadline] [category]")
		}
		var deadline, category string
		if len(args) > 2 {
			deadline = args[2]
		}
		if len(args) > 3 {
			category = args[3]
		}
		t := s.Add(args[1], category, deadline, today, now)
		if err := s.Save(); err != nil {
			return err
		}
		fmt.Printf("Task added: #%d %s\n", t.ID, t.Title)

	case "list":
		day := today
		if len(args) > 1 {
			if _, err := parseDate(args[1]); err != nil {
				return fmt.Errorf("invalid date %q (use YYYY-MM-DD)", args[1])
			}
			day = args[1]
		}
		tasks := s.TasksOn(day)
		if len(tasks) == 0 {
			fmt.Println("No tasks found for", day)
			return nil
		}
		for _, t := range tasks {
			status := "[ ]"
			if t.Done {
				status = "[x]"
			}
			line := fmt.Sprintf("%3d. %s %s", t.ID, status, t.Title)
			if t.Category != "" {
				line += " #" + t.Category
			}
			if t.Deadline != "" {
				line += " (deadline " + t.Deadline + ")"
			}
			if d := s.TaskTime(t, now); d > 0 {
				line += " — " + fmtDur(d)
			}
			if s.Active != nil && s.Active.TaskID == t.ID {
				line += " ⏱ running"
			}
			fmt.Println(line)
		}

	case "done", "delete", "start":
		if len(args) < 2 {
			return fmt.Errorf("please provide a task ID")
		}
		id, err := strconv.Atoi(args[1])
		if err != nil {
			return fmt.Errorf("invalid task ID %q", args[1])
		}
		switch args[0] {
		case "done":
			err = s.SetDone(id, true, now)
		case "delete":
			err = s.Delete(id)
		case "start":
			err = s.StartTimer(id, 0, now)
		}
		if err != nil {
			return err
		}
		if err := s.Save(); err != nil {
			return err
		}
		fmt.Printf("OK: %s %d\n", args[0], id)

	case "stop":
		if s.Active == nil {
			fmt.Println("No timer was running.")
			return nil
		}
		d := s.StopTimer(now)
		if err := s.Save(); err != nil {
			return err
		}
		fmt.Println("Timer stopped —", fmtDur(d), "recorded.")

	case "report":
		day := today
		if len(args) > 1 {
			if _, err := parseDate(args[1]); err != nil {
				return fmt.Errorf("invalid date %q (use YYYY-MM-DD)", args[1])
			}
			day = args[1]
		}
		fmt.Print(BuildReport(s, day, now).Markdown())

	case "history":
		days := 14
		if len(args) > 1 {
			if n, err := strconv.Atoi(args[1]); err == nil && n > 0 {
				days = n
			}
		}
		fmt.Printf("%-12s %-4s %-7s %8s\n", "Date", "Day", "Done", "Focus")
		for _, h := range s.History(today, days, now) {
			t, _ := parseDate(h.Date)
			fmt.Printf("%-12s %-4s %-7s %8s\n", h.Date, t.Format("Mon"),
				fmt.Sprintf("%d/%d", h.Done, h.Total), fmtDur(h.Focus))
		}

	case "help", "-h", "--help":
		fmt.Print(usage)

	default:
		return fmt.Errorf("unknown command %q\n\n%s", args[0], usage)
	}
	return nil
}
