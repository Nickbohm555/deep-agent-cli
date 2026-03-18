package session

import "time"

type Session struct {
	ThreadID  string
	RepoRoot  string
	CreatedAt time.Time
}

type Message struct {
	ID        int64
	ThreadID  string
	Role      string
	Content   string
	CreatedAt time.Time
}
