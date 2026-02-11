package models

// Channel represents a chat channel within a category.
type Channel struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	CategoryID string   `json:"category_id"`
	Position   int      `json:"position"` // Ordering within the category
	AllowList  []string `json:"allow_list,omitempty"` // User IDs or group IDs allowed
	DenyList   []string `json:"deny_list,omitempty"`  // User IDs or group IDs denied
}
