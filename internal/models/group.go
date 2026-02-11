package models

// Group represents a user group for access control.
type Group struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Members []string `json:"members"` // Tailscale user IDs
}
