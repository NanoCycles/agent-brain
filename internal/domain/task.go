package domain

type Task struct {
	ID      string
	Path    string
	Title   string
	Content string
	Topics  []string
}
