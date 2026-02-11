package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/MattCruikshank/tambua/internal/models"
	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
)

// ServerDB handles server-side database operations.
type ServerDB struct {
	db *sql.DB
}

// NewServerDB opens or creates the server database.
func NewServerDB(path string) (*ServerDB, error) {
	db, err := sql.Open("sqlite3", path+"?_fk=on")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	sdb := &ServerDB{db: db}
	if err := sdb.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}

	return sdb, nil
}

// Close closes the database connection.
func (s *ServerDB) Close() error {
	return s.db.Close()
}

func (s *ServerDB) migrate() error {
	schema := `
		CREATE TABLE IF NOT EXISTS server_info (
			id TEXT PRIMARY KEY,
			display_name TEXT NOT NULL,
			description TEXT,
			icon_url TEXT
		);

		CREATE TABLE IF NOT EXISTS categories (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			position INTEGER NOT NULL DEFAULT 0
		);

		CREATE TABLE IF NOT EXISTS channels (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			category_id TEXT NOT NULL REFERENCES categories(id) ON DELETE CASCADE,
			position INTEGER NOT NULL DEFAULT 0,
			allow_list TEXT, -- JSON array
			deny_list TEXT   -- JSON array
		);

		CREATE TABLE IF NOT EXISTS messages (
			id TEXT PRIMARY KEY,
			channel_id TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
			author_id TEXT NOT NULL,
			author_display_name TEXT NOT NULL DEFAULT '',
			author_login_name TEXT NOT NULL DEFAULT '',
			content TEXT NOT NULL,
			timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);

		CREATE INDEX IF NOT EXISTS idx_messages_channel ON messages(channel_id, timestamp);

		CREATE TABLE IF NOT EXISTS groups (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL UNIQUE
		);

		CREATE TABLE IF NOT EXISTS group_members (
			group_id TEXT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
			user_id TEXT NOT NULL,
			PRIMARY KEY (group_id, user_id)
		);
	`
	if _, err := s.db.Exec(schema); err != nil {
		return err
	}

	// Migration for existing tables (ignore errors if columns exist)
	s.db.Exec(`ALTER TABLE messages ADD COLUMN author_display_name TEXT NOT NULL DEFAULT ''`)
	s.db.Exec(`ALTER TABLE messages ADD COLUMN author_login_name TEXT NOT NULL DEFAULT ''`)

	return nil
}

// GetServerInfo retrieves server metadata.
func (s *ServerDB) GetServerInfo() (*models.Server, error) {
	var srv models.Server
	err := s.db.QueryRow(`SELECT id, display_name, COALESCE(description, ''), COALESCE(icon_url, '') FROM server_info LIMIT 1`).
		Scan(&srv.ID, &srv.DisplayName, &srv.Description, &srv.IconURL)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &srv, err
}

// SetServerInfo sets or updates server metadata.
func (s *ServerDB) SetServerInfo(srv *models.Server) error {
	_, err := s.db.Exec(`
		INSERT INTO server_info (id, display_name, description, icon_url)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			display_name = excluded.display_name,
			description = excluded.description,
			icon_url = excluded.icon_url
	`, srv.ID, srv.DisplayName, srv.Description, srv.IconURL)
	return err
}

// CreateCategory creates a new category.
func (s *ServerDB) CreateCategory(name string, position int) (*models.Category, error) {
	id := uuid.New().String()
	_, err := s.db.Exec(`INSERT INTO categories (id, name, position) VALUES (?, ?, ?)`, id, name, position)
	if err != nil {
		return nil, err
	}
	return &models.Category{ID: id, Name: name, Position: position}, nil
}

// GetCategories returns all categories ordered by position.
func (s *ServerDB) GetCategories() ([]models.Category, error) {
	rows, err := s.db.Query(`SELECT id, name, position FROM categories ORDER BY position`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var categories []models.Category
	for rows.Next() {
		var c models.Category
		if err := rows.Scan(&c.ID, &c.Name, &c.Position); err != nil {
			return nil, err
		}
		categories = append(categories, c)
	}
	return categories, rows.Err()
}

// UpdateCategory updates a category.
func (s *ServerDB) UpdateCategory(cat *models.Category) error {
	_, err := s.db.Exec(`UPDATE categories SET name = ?, position = ? WHERE id = ?`,
		cat.Name, cat.Position, cat.ID)
	return err
}

// DeleteCategory deletes a category and its channels.
func (s *ServerDB) DeleteCategory(id string) error {
	_, err := s.db.Exec(`DELETE FROM categories WHERE id = ?`, id)
	return err
}

// CreateChannel creates a new channel.
func (s *ServerDB) CreateChannel(name, categoryID string, position int) (*models.Channel, error) {
	id := uuid.New().String()
	_, err := s.db.Exec(`INSERT INTO channels (id, name, category_id, position) VALUES (?, ?, ?, ?)`,
		id, name, categoryID, position)
	if err != nil {
		return nil, err
	}
	return &models.Channel{ID: id, Name: name, CategoryID: categoryID, Position: position}, nil
}

// GetChannels returns all channels for a category ordered by position.
func (s *ServerDB) GetChannels(categoryID string) ([]models.Channel, error) {
	rows, err := s.db.Query(`
		SELECT id, name, category_id, position, COALESCE(allow_list, '[]'), COALESCE(deny_list, '[]')
		FROM channels WHERE category_id = ? ORDER BY position
	`, categoryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var channels []models.Channel
	for rows.Next() {
		var c models.Channel
		var allowJSON, denyJSON string
		if err := rows.Scan(&c.ID, &c.Name, &c.CategoryID, &c.Position, &allowJSON, &denyJSON); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(allowJSON), &c.AllowList)
		json.Unmarshal([]byte(denyJSON), &c.DenyList)
		channels = append(channels, c)
	}
	return channels, rows.Err()
}

// GetChannel returns a single channel by ID.
func (s *ServerDB) GetChannel(id string) (*models.Channel, error) {
	var c models.Channel
	var allowJSON, denyJSON string
	err := s.db.QueryRow(`
		SELECT id, name, category_id, position, COALESCE(allow_list, '[]'), COALESCE(deny_list, '[]')
		FROM channels WHERE id = ?
	`, id).Scan(&c.ID, &c.Name, &c.CategoryID, &c.Position, &allowJSON, &denyJSON)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	json.Unmarshal([]byte(allowJSON), &c.AllowList)
	json.Unmarshal([]byte(denyJSON), &c.DenyList)
	return &c, nil
}

// UpdateChannel updates a channel.
func (s *ServerDB) UpdateChannel(ch *models.Channel) error {
	allowJSON, _ := json.Marshal(ch.AllowList)
	denyJSON, _ := json.Marshal(ch.DenyList)
	_, err := s.db.Exec(`
		UPDATE channels SET name = ?, category_id = ?, position = ?, allow_list = ?, deny_list = ?
		WHERE id = ?
	`, ch.Name, ch.CategoryID, ch.Position, string(allowJSON), string(denyJSON), ch.ID)
	return err
}

// DeleteChannel deletes a channel and its messages.
func (s *ServerDB) DeleteChannel(id string) error {
	_, err := s.db.Exec(`DELETE FROM channels WHERE id = ?`, id)
	return err
}

// ClearChannelMessages removes all messages from a channel.
func (s *ServerDB) ClearChannelMessages(channelID string) error {
	_, err := s.db.Exec(`DELETE FROM messages WHERE channel_id = ?`, channelID)
	return err
}

// CreateMessage creates a new message.
func (s *ServerDB) CreateMessage(channelID string, author *models.User, content string) (*models.Message, error) {
	id := uuid.New().String()
	now := time.Now().UTC()
	displayName := ""
	loginName := ""
	if author != nil {
		displayName = author.DisplayName
		loginName = author.LoginName
	}
	_, err := s.db.Exec(`INSERT INTO messages (id, channel_id, author_id, author_display_name, author_login_name, content, timestamp) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, channelID, author.ID, displayName, loginName, content, now)
	if err != nil {
		return nil, err
	}
	return &models.Message{
		ID:        id,
		ChannelID: channelID,
		AuthorID:  author.ID,
		Content:   content,
		Timestamp: now,
	}, nil
}

// GetMessages returns messages for a channel, optionally with pagination.
func (s *ServerDB) GetMessages(channelID string, limit int, before *time.Time) ([]models.Message, error) {
	var rows *sql.Rows
	var err error
	if before != nil {
		rows, err = s.db.Query(`
			SELECT id, channel_id, author_id, content, timestamp
			FROM messages WHERE channel_id = ? AND timestamp < ?
			ORDER BY timestamp DESC LIMIT ?
		`, channelID, before, limit)
	} else {
		rows, err = s.db.Query(`
			SELECT id, channel_id, author_id, content, timestamp
			FROM messages WHERE channel_id = ?
			ORDER BY timestamp DESC LIMIT ?
		`, channelID, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []models.Message
	for rows.Next() {
		var m models.Message
		if err := rows.Scan(&m.ID, &m.ChannelID, &m.AuthorID, &m.Content, &m.Timestamp); err != nil {
			return nil, err
		}
		messages = append(messages, m)
	}
	// Reverse to get chronological order
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}
	return messages, rows.Err()
}

// MessageWithAuthor holds a message with its author info.
type MessageWithAuthor struct {
	Message models.Message
	Author  models.User
}

// GetMessagesWithAuthors returns messages with author info for a channel.
// If beforeID is provided, returns messages older than that message.
func (s *ServerDB) GetMessagesWithAuthors(channelID string, limit int, beforeID string) ([]MessageWithAuthor, error) {
	var rows *sql.Rows
	var err error

	if beforeID != "" {
		// Get the timestamp of the "before" message
		var beforeTime time.Time
		err = s.db.QueryRow(`SELECT timestamp FROM messages WHERE id = ?`, beforeID).Scan(&beforeTime)
		if err != nil {
			return nil, err
		}
		rows, err = s.db.Query(`
			SELECT id, channel_id, author_id, author_display_name, author_login_name, content, timestamp
			FROM messages WHERE channel_id = ? AND timestamp < ?
			ORDER BY timestamp DESC LIMIT ?
		`, channelID, beforeTime, limit)
	} else {
		rows, err = s.db.Query(`
			SELECT id, channel_id, author_id, author_display_name, author_login_name, content, timestamp
			FROM messages WHERE channel_id = ?
			ORDER BY timestamp DESC LIMIT ?
		`, channelID, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []MessageWithAuthor
	for rows.Next() {
		var m MessageWithAuthor
		if err := rows.Scan(&m.Message.ID, &m.Message.ChannelID, &m.Message.AuthorID,
			&m.Author.DisplayName, &m.Author.LoginName, &m.Message.Content, &m.Message.Timestamp); err != nil {
			return nil, err
		}
		m.Author.ID = m.Message.AuthorID
		messages = append(messages, m)
	}
	// Reverse to get chronological order
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}
	return messages, rows.Err()
}

// CreateGroup creates a new group.
func (s *ServerDB) CreateGroup(name string) (*models.Group, error) {
	id := uuid.New().String()
	_, err := s.db.Exec(`INSERT INTO groups (id, name) VALUES (?, ?)`, id, name)
	if err != nil {
		return nil, err
	}
	return &models.Group{ID: id, Name: name}, nil
}

// GetGroups returns all groups with their members.
func (s *ServerDB) GetGroups() ([]models.Group, error) {
	rows, err := s.db.Query(`SELECT id, name FROM groups ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var groups []models.Group
	for rows.Next() {
		var g models.Group
		if err := rows.Scan(&g.ID, &g.Name); err != nil {
			return nil, err
		}
		groups = append(groups, g)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Load members for each group
	for i := range groups {
		members, err := s.GetGroupMembers(groups[i].ID)
		if err != nil {
			return nil, err
		}
		groups[i].Members = members
	}
	return groups, nil
}

// GetGroupMembers returns the member IDs of a group.
func (s *ServerDB) GetGroupMembers(groupID string) ([]string, error) {
	rows, err := s.db.Query(`SELECT user_id FROM group_members WHERE group_id = ?`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var members []string
	for rows.Next() {
		var userID string
		if err := rows.Scan(&userID); err != nil {
			return nil, err
		}
		members = append(members, userID)
	}
	return members, rows.Err()
}

// AddGroupMember adds a user to a group.
func (s *ServerDB) AddGroupMember(groupID, userID string) error {
	_, err := s.db.Exec(`INSERT OR IGNORE INTO group_members (group_id, user_id) VALUES (?, ?)`, groupID, userID)
	return err
}

// RemoveGroupMember removes a user from a group.
func (s *ServerDB) RemoveGroupMember(groupID, userID string) error {
	_, err := s.db.Exec(`DELETE FROM group_members WHERE group_id = ? AND user_id = ?`, groupID, userID)
	return err
}

// DeleteGroup deletes a group.
func (s *ServerDB) DeleteGroup(id string) error {
	_, err := s.db.Exec(`DELETE FROM groups WHERE id = ?`, id)
	return err
}

// GetUserGroups returns all group IDs that a user belongs to.
func (s *ServerDB) GetUserGroups(userID string) ([]string, error) {
	rows, err := s.db.Query(`SELECT group_id FROM group_members WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var groupIDs []string
	for rows.Next() {
		var groupID string
		if err := rows.Scan(&groupID); err != nil {
			return nil, err
		}
		groupIDs = append(groupIDs, groupID)
	}
	return groupIDs, rows.Err()
}
