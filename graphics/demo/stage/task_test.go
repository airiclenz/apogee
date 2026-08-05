package taskman

import "testing"

func TestPendingIncludesFirstTask(t *testing.T) {
	tasks := []Task{
		{Title: "write the proposal"},
		{Title: "review the diff"},
		{Title: "ship v0.11", Done: true},
	}
	got := Pending(tasks)
	if len(got) != 2 {
		t.Fatalf("Pending returned %d tasks, want 2", len(got))
	}
	if got[0].Title != "write the proposal" {
		t.Fatalf("first pending task = %q, want %q", got[0].Title, "write the proposal")
	}
}
