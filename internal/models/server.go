package models

import "time"

// Server represents a tambua-server instance metadata.
type Server struct {
	ID          string `json:"id"`
	Hostname    string `json:"hostname"`     // Tailscale hostname
	DisplayName string `json:"display_name"` // Human-readable name
	Description string `json:"description"`
	IconURL     string `json:"icon_url"`
}

// EnrolledServer represents a server the client has connected to.
type EnrolledServer struct {
	ID            string    `json:"id"`
	Hostname      string    `json:"hostname"`
	DisplayName   string    `json:"display_name"`
	LastConnected time.Time `json:"last_connected"`
}
