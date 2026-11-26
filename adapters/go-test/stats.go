package main

import (
	"encoding/json"
	"strings"
)

type testEvent struct {
	Action string `json:"Action"`
	Test   string `json:"Test"`
}

type tally struct {
	executed int
	skipped  int
}

type testNode struct {
	action string
	parent bool
}

// tallyEvents counts leaf tests. A parent that only wraps subtests is not an
// extra execution when it passes. A failing parent is counted, because the
// event stream cannot prove the failure was only in a child. Package events
// without a test name do not count. All-skip and no-match therefore report
// executed 0.
func tallyEvents(lines []string) tally {
	nodes := map[string]*testNode{}
	var order []string
	touch := func(name string) *testNode {
		n, ok := nodes[name]
		if !ok {
			n = &testNode{}
			nodes[name] = n
			order = append(order, name)
		}
		return n
	}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || line[0] != '{' {
			continue
		}
		var ev testEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil || ev.Test == "" {
			continue
		}
		n := touch(ev.Test)
		switch ev.Action {
		case "pass", "fail", "skip":
			n.action = ev.Action
		}
		if i := strings.LastIndex(ev.Test, "/"); i >= 0 {
			touch(ev.Test[:i]).parent = true
		}
	}
	var out tally
	for _, name := range order {
		n := nodes[name]
		if n.parent {
			if n.action == "fail" {
				out.executed++
			}
			continue
		}
		switch n.action {
		case "pass", "fail":
			out.executed++
		case "skip":
			out.skipped++
		}
	}
	return out
}
