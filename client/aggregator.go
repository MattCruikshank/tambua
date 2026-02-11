package client

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/MattCruikshank/tambua/internal/db"
	"github.com/MattCruikshank/tambua/internal/models"
	"github.com/MattCruikshank/tambua/internal/protocol"
	"github.com/gorilla/websocket"
	"tailscale.com/tsnet"
)

// ServerConnection represents a connection to a tambua-server.
type ServerConnection struct {
	server     *models.EnrolledServer
	conn       *websocket.Conn
	user       *models.User
	serverInfo *models.Server
	categories []protocol.CategoryWithChannels
	send       chan []byte
	done       chan struct{}
	mu         sync.RWMutex
}

// Aggregator manages connections to multiple servers.
type Aggregator struct {
	db          *db.ClientDB
	tsServer    *tsnet.Server
	dialer      *websocket.Dialer
	connections map[string]*ServerConnection // serverID -> connection
	connMu      sync.RWMutex
	onMessage   func(serverID string, msg *protocol.MessageMessage)
	onUpdate    func(serverID string) // Called when channel list updates
}

// NewAggregator creates a new aggregator.
func NewAggregator(database *db.ClientDB, tsServer *tsnet.Server) *Aggregator {
	return &Aggregator{
		db:          database,
		tsServer:    tsServer,
		connections: make(map[string]*ServerConnection),
		dialer: &websocket.Dialer{
			NetDialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return tsServer.Dial(ctx, network, addr)
			},
			TLSClientConfig: &tls.Config{},
		},
	}
}

// SetMessageHandler sets the callback for incoming messages.
func (a *Aggregator) SetMessageHandler(handler func(serverID string, msg *protocol.MessageMessage)) {
	a.onMessage = handler
}

// SetUpdateHandler sets the callback for server updates.
func (a *Aggregator) SetUpdateHandler(handler func(serverID string)) {
	a.onUpdate = handler
}

// normalizeHostname extracts a hostname from a URL or cleans up a hostname.
// Handles inputs like "https://server.tailnet.ts.net/" or "server.tailnet.ts.net".
func normalizeHostname(input string) string {
	h := strings.TrimSpace(input)
	h = strings.TrimPrefix(h, "https://")
	h = strings.TrimPrefix(h, "http://")
	h = strings.TrimSuffix(h, "/")
	// Remove any path component
	if idx := strings.Index(h, "/"); idx != -1 {
		h = h[:idx]
	}
	return h
}

// Connect connects to a server by hostname.
func (a *Aggregator) Connect(ctx context.Context, hostname string) error {
	hostname = normalizeHostname(hostname)

	// Check if already connected
	a.connMu.RLock()
	for _, conn := range a.connections {
		if conn.server.Hostname == hostname {
			a.connMu.RUnlock()
			return nil // Already connected
		}
	}
	a.connMu.RUnlock()

	// Enroll the server
	enrolled, err := a.db.EnrollServer(hostname, hostname)
	if err != nil {
		return fmt.Errorf("failed to enroll server: %w", err)
	}

	// Connect via WebSocket through tsnet
	wsURL := fmt.Sprintf("wss://%s/ws", hostname)
	conn, _, err := a.dialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return fmt.Errorf("failed to connect to %s: %w", hostname, err)
	}

	sc := &ServerConnection{
		server: enrolled,
		conn:   conn,
		send:   make(chan []byte, 256),
		done:   make(chan struct{}),
	}

	a.connMu.Lock()
	a.connections[enrolled.ID] = sc
	a.connMu.Unlock()

	// Start read/write pumps
	go a.writePump(sc)
	go a.readPump(sc)

	// Update last connected
	a.db.UpdateLastConnected(hostname)

	log.Printf("Connected to server: %s", hostname)
	return nil
}

// Disconnect disconnects from a server.
func (a *Aggregator) Disconnect(serverID string) {
	a.connMu.Lock()
	conn, ok := a.connections[serverID]
	if ok {
		delete(a.connections, serverID)
	}
	a.connMu.Unlock()

	if ok {
		close(conn.done)
		conn.conn.Close()
		log.Printf("Disconnected from server: %s", conn.server.Hostname)
	}
}

// GetConnection returns a server connection.
func (a *Aggregator) GetConnection(serverID string) *ServerConnection {
	a.connMu.RLock()
	defer a.connMu.RUnlock()
	return a.connections[serverID]
}

// GetConnections returns all active connections.
func (a *Aggregator) GetConnections() []*ServerConnection {
	a.connMu.RLock()
	defer a.connMu.RUnlock()

	conns := make([]*ServerConnection, 0, len(a.connections))
	for _, conn := range a.connections {
		conns = append(conns, conn)
	}
	return conns
}

// Subscribe subscribes to a channel on a server.
func (a *Aggregator) Subscribe(serverID, channelID string) error {
	conn := a.GetConnection(serverID)
	if conn == nil {
		return fmt.Errorf("not connected to server %s", serverID)
	}

	env, err := protocol.NewEnvelope(protocol.TypeSubscribe, protocol.SubscribeMessage{
		ChannelID: channelID,
	})
	if err != nil {
		return err
	}

	data, err := json.Marshal(env)
	if err != nil {
		return err
	}

	conn.send <- data
	return nil
}

// Unsubscribe unsubscribes from a channel on a server.
func (a *Aggregator) Unsubscribe(serverID, channelID string) error {
	conn := a.GetConnection(serverID)
	if conn == nil {
		return fmt.Errorf("not connected to server %s", serverID)
	}

	env, err := protocol.NewEnvelope(protocol.TypeUnsubscribe, protocol.UnsubscribeMessage{
		ChannelID: channelID,
	})
	if err != nil {
		return err
	}

	data, err := json.Marshal(env)
	if err != nil {
		return err
	}

	conn.send <- data
	return nil
}

// SendMessage sends a message to a channel on a server.
func (a *Aggregator) SendMessage(serverID, channelID, content string) error {
	conn := a.GetConnection(serverID)
	if conn == nil {
		return fmt.Errorf("not connected to server %s", serverID)
	}

	env, err := protocol.NewEnvelope(protocol.TypeSendMessage, protocol.SendMessageMessage{
		ChannelID: channelID,
		Content:   content,
	})
	if err != nil {
		return err
	}

	data, err := json.Marshal(env)
	if err != nil {
		return err
	}

	conn.send <- data
	return nil
}

func (a *Aggregator) readPump(sc *ServerConnection) {
	defer func() {
		a.Disconnect(sc.server.ID)
	}()

	sc.conn.SetReadLimit(65536)
	sc.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	sc.conn.SetPongHandler(func(string) error {
		sc.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, message, err := sc.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error for %s: %v", sc.server.Hostname, err)
			}
			return
		}

		a.handleMessage(sc, message)
	}
}

func (a *Aggregator) writePump(sc *ServerConnection) {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		sc.conn.Close()
	}()

	for {
		select {
		case message, ok := <-sc.send:
			sc.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				sc.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if err := sc.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}

		case <-ticker.C:
			sc.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := sc.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}

		case <-sc.done:
			return
		}
	}
}

func (a *Aggregator) handleMessage(sc *ServerConnection, data []byte) {
	env, err := protocol.ParseEnvelope(data)
	if err != nil {
		log.Printf("Failed to parse message from %s: %v", sc.server.Hostname, err)
		return
	}

	switch env.Type {
	case protocol.TypeAuthOK:
		var msg protocol.AuthOKMessage
		if err := json.Unmarshal(env.Data, &msg); err != nil {
			log.Printf("Failed to parse auth_ok: %v", err)
			return
		}
		sc.mu.Lock()
		sc.user = &msg.User
		sc.serverInfo = &msg.Server
		sc.server.DisplayName = msg.Server.DisplayName
		sc.mu.Unlock()

		// Update display name in DB
		a.db.EnrollServer(sc.server.Hostname, msg.Server.DisplayName)
		log.Printf("Authenticated to %s as %s", sc.server.Hostname, msg.User.LoginName)

	case protocol.TypeChannelList:
		var msg protocol.ChannelListMessage
		if err := json.Unmarshal(env.Data, &msg); err != nil {
			log.Printf("Failed to parse channel_list: %v", err)
			return
		}
		sc.mu.Lock()
		sc.categories = msg.Categories
		sc.mu.Unlock()

		if a.onUpdate != nil {
			a.onUpdate(sc.server.ID)
		}

	case protocol.TypeMessage:
		var msg protocol.MessageMessage
		if err := json.Unmarshal(env.Data, &msg); err != nil {
			log.Printf("Failed to parse message: %v", err)
			return
		}

		// Cache the message
		a.db.CacheMessage(&models.CachedMessage{
			ServerID:  sc.server.ID,
			ChannelID: msg.ChannelID,
			MessageID: msg.Message.ID,
			AuthorID:  msg.Message.AuthorID,
			Content:   msg.Message.Content,
			Timestamp: msg.Message.Timestamp,
		})

		if a.onMessage != nil {
			a.onMessage(sc.server.ID, &msg)
		}

	case protocol.TypeError:
		var msg protocol.ErrorMessage
		if err := json.Unmarshal(env.Data, &msg); err != nil {
			log.Printf("Failed to parse error: %v", err)
			return
		}
		log.Printf("Error from %s: [%s] %s", sc.server.Hostname, msg.Code, msg.Message)
	}
}

// GetServerInfo returns the server info for a connection.
func (sc *ServerConnection) GetServerInfo() *models.Server {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	return sc.serverInfo
}

// GetCategories returns the categories for a connection.
func (sc *ServerConnection) GetCategories() []protocol.CategoryWithChannels {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	return sc.categories
}

// GetUser returns the current user for a connection.
func (sc *ServerConnection) GetUser() *models.User {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	return sc.user
}

// GetEnrolledServer returns the enrolled server info.
func (sc *ServerConnection) GetEnrolledServer() *models.EnrolledServer {
	return sc.server
}
