package models

// Category represents a group of channels.
type Category struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Position int    `json:"position"` // Ordering within the server
}
