package models

// User represents a Tailscale identity.
type User struct {
	ID          string `json:"id"`           // Tailscale user ID
	LoginName   string `json:"login_name"`   // e.g., "user@example.com"
	DisplayName string `json:"display_name"` // e.g., "John Doe"
	ProfilePic  string `json:"profile_pic"`  // URL to profile picture
}
