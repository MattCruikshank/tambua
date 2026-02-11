package db

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/MattCruikshank/tambua/internal/models"
	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
)

// ClientDB handles client-side database operations.
type ClientDB struct {
	db *sql.DB
}

// NewClientDB opens or creates the client database.
func NewClientDB(path string) (*ClientDB, error) {
	db, err := sql.Open("sqlite3", path+"?_fk=on")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	cdb := &ClientDB{db: db}
	if err := cdb.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}

	return cdb, nil
}

// Close closes the database connection.
func (c *ClientDB) Close() error {
	return c.db.Close()
}

func (c *ClientDB) migrate() error {
	schema := `
		CREATE TABLE IF NOT EXISTS enrolled_servers (
			id TEXT PRIMARY KEY,
			hostname TEXT NOT NULL UNIQUE,
			display_name TEXT NOT NULL,
			last_connected DATETIME
		);

		CREATE TABLE IF NOT EXISTS preferences (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);

		CREATE TABLE IF NOT EXISTS cached_messages (
			server_id TEXT NOT NULL,
			channel_id TEXT NOT NULL,
			message_id TEXT NOT NULL,
			author_id TEXT NOT NULL,
			content TEXT NOT NULL,
			timestamp DATETIME NOT NULL,
			PRIMARY KEY (server_id, message_id)
		);

		CREATE INDEX IF NOT EXISTS idx_cached_messages_channel
			ON cached_messages(server_id, channel_id, timestamp);
	`
	_, err := c.db.Exec(schema)
	return err
}

// EnrollServer adds or updates an enrolled server.
func (c *ClientDB) EnrollServer(hostname, displayName string) (*models.EnrolledServer, error) {
	now := time.Now().UTC()
	id := uuid.New().String()

	_, err := c.db.Exec(`
		INSERT INTO enrolled_servers (id, hostname, display_name, last_connected)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(hostname) DO UPDATE SET
			display_name = excluded.display_name,
			last_connected = excluded.last_connected
	`, id, hostname, displayName, now)
	if err != nil {
		return nil, err
	}

	// Fetch the actual ID (might be existing)
	var srv models.EnrolledServer
	err = c.db.QueryRow(`SELECT id, hostname, display_name, last_connected FROM enrolled_servers WHERE hostname = ?`, hostname).
		Scan(&srv.ID, &srv.Hostname, &srv.DisplayName, &srv.LastConnected)
	return &srv, err
}

// GetEnrolledServers returns all enrolled servers.
func (c *ClientDB) GetEnrolledServers() ([]models.EnrolledServer, error) {
	rows, err := c.db.Query(`SELECT id, hostname, display_name, last_connected FROM enrolled_servers ORDER BY display_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var servers []models.EnrolledServer
	for rows.Next() {
		var s models.EnrolledServer
		var lastConnected sql.NullTime
		if err := rows.Scan(&s.ID, &s.Hostname, &s.DisplayName, &lastConnected); err != nil {
			return nil, err
		}
		if lastConnected.Valid {
			s.LastConnected = lastConnected.Time
		}
		servers = append(servers, s)
	}
	return servers, rows.Err()
}

// GetEnrolledServer returns a single enrolled server by ID.
func (c *ClientDB) GetEnrolledServer(id string) (*models.EnrolledServer, error) {
	var s models.EnrolledServer
	var lastConnected sql.NullTime
	err := c.db.QueryRow(`SELECT id, hostname, display_name, last_connected FROM enrolled_servers WHERE id = ?`, id).
		Scan(&s.ID, &s.Hostname, &s.DisplayName, &lastConnected)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if lastConnected.Valid {
		s.LastConnected = lastConnected.Time
	}
	return &s, nil
}

// RemoveEnrolledServer removes an enrolled server.
func (c *ClientDB) RemoveEnrolledServer(id string) error {
	_, err := c.db.Exec(`DELETE FROM enrolled_servers WHERE id = ?`, id)
	return err
}

// UpdateLastConnected updates the last connected time for a server.
func (c *ClientDB) UpdateLastConnected(hostname string) error {
	_, err := c.db.Exec(`UPDATE enrolled_servers SET last_connected = ? WHERE hostname = ?`, time.Now().UTC(), hostname)
	return err
}

// GetPreference retrieves a preference value.
func (c *ClientDB) GetPreference(key string) (string, error) {
	var value string
	err := c.db.QueryRow(`SELECT value FROM preferences WHERE key = ?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

// SetPreference sets a preference value.
func (c *ClientDB) SetPreference(key, value string) error {
	_, err := c.db.Exec(`
		INSERT INTO preferences (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`, key, value)
	return err
}

// CacheMessage caches a message locally.
func (c *ClientDB) CacheMessage(msg *models.CachedMessage) error {
	_, err := c.db.Exec(`
		INSERT OR REPLACE INTO cached_messages
			(server_id, channel_id, message_id, author_id, content, timestamp)
		VALUES (?, ?, ?, ?, ?, ?)
	`, msg.ServerID, msg.ChannelID, msg.MessageID, msg.AuthorID, msg.Content, msg.Timestamp)
	return err
}

// GetCachedMessages retrieves cached messages for a channel.
func (c *ClientDB) GetCachedMessages(serverID, channelID string, limit int) ([]models.CachedMessage, error) {
	rows, err := c.db.Query(`
		SELECT server_id, channel_id, message_id, author_id, content, timestamp
		FROM cached_messages
		WHERE server_id = ? AND channel_id = ?
		ORDER BY timestamp DESC LIMIT ?
	`, serverID, channelID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []models.CachedMessage
	for rows.Next() {
		var m models.CachedMessage
		if err := rows.Scan(&m.ServerID, &m.ChannelID, &m.MessageID, &m.AuthorID, &m.Content, &m.Timestamp); err != nil {
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

// ClearCachedMessages clears cached messages for a server.
func (c *ClientDB) ClearCachedMessages(serverID string) error {
	_, err := c.db.Exec(`DELETE FROM cached_messages WHERE server_id = ?`, serverID)
	return err
}
