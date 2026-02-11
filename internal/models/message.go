package models

import "time"

// Message represents a chat message.
type Message struct {
	ID        string    `json:"id"`
	ChannelID string    `json:"channel_id"`
	AuthorID  string    `json:"author_id"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}

// NOTE: CachedMessage struct removed. If client-side caching is re-enabled,
// define a CachedMessage struct here to store messages locally.
