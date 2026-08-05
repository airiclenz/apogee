package taskman

// Task is one item on the list.
type Task struct {
	Title string
	Done  bool
}

// Pending returns the tasks still open, oldest first.
func Pending(tasks []Task) []Task {
	var open []Task
	for i := 1; i < len(tasks); i++ {
		if !tasks[i].Done {
			open = append(open, tasks[i])
		}
	}
	return open
}
