# Task Manager

A terminal task manager written in **Go**, with an interactive dashboard
built on [Bubble Tea](https://github.com/charmbracelet/bubbletea).

* **History**: when the app opens, it shows a table of your past days: tasks done, a progress bar, and focus time.
* **Today**: write tasks, tick them off, and start a timer or a 25‑minute pomodoro on any task.
* **Report**: a daily report with completion rate, focus time, work span, a per‑category breakdown, and done / not‑done lists. You can export it to Markdown.

---

## Installation

```bash
git clone https://github.com/yourusername/taskmanager.git
cd taskmanager
go build -o taskmanager .
# optional: put it on your PATH
mv taskmanager /usr/local/bin/
```

To open it automatically in every new terminal, add this to `~/.zshrc`:

```bash
[[ -o interactive ]] && command -v taskmanager >/dev/null && taskmanager
```

---

## Interactive dashboard

```bash
taskmanager
```

| View | Keys |
|---|---|
| Everywhere | `1` `2` `3` / `tab` switch view · `t` today · `S` stop timer · `q` quit |
| **History** | `↑↓` select day · `enter` open day · `r` report of that day · `w` 7 / 14 / 30 days |
| **Today** | `a` add · `space` done/undone · `enter` start/stop timer · `p` pomodoro (25m) · `e` edit · `d` delete · `[` `]` previous/next day · `c` bring unfinished tasks from earlier days · `r` report |
| **Report** | `[` `]` previous/next day · `x` export to Markdown · `enter` open day |

When you add a task, put a category at the end with `#`, for example `Write the report #work`.

The timer is saved to disk, so it keeps running after you close the app.
When a pomodoro ends you get a bell and a desktop notification.

---

## CLI commands

```bash
taskmanager add "Learn Go" "2025-09-25" "Programming"   # title [deadline] [category]
taskmanager list [YYYY-MM-DD]
taskmanager done 1
taskmanager delete 1
taskmanager start 1          # start a timer
taskmanager stop             # stop the timer
taskmanager report [YYYY-MM-DD]
taskmanager history [days]
```

---

## Run with Docker

```bash
docker build -t taskmanager .
docker run -it --rm -v ~/.taskmanager:/root/.taskmanager taskmanager
```

---

## Data storage

All data is stored in `~/.taskmanager/data.json`. Set `TASKMANAGER_DATA` to use another path.
Exported reports go to `~/.taskmanager/reports/YYYY-MM-DD.md`.
