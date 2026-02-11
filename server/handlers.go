package server

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/MattCruikshank/tambua/internal/auth"
	"github.com/MattCruikshank/tambua/internal/db"
	"github.com/MattCruikshank/tambua/internal/models"
	"github.com/MattCruikshank/tambua/internal/protocol"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// Server holds the server's dependencies.
type Server struct {
	hub      *Hub
	db       *db.ServerDB
	auth     *auth.Authenticator
	hostname string
}

// NewServer creates a new server instance.
func NewServer(hub *Hub, database *db.ServerDB, authenticator *auth.Authenticator, hostname string) *Server {
	return &Server{
		hub:      hub,
		db:       database,
		auth:     authenticator,
		hostname: hostname,
	}
}

// HandleWebSocket handles WebSocket connections.
func (s *Server) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	user, err := s.auth.GetUser(r.Context(), r)
	if err != nil {
		log.Printf("Auth failed: %v", err)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}

	client := s.hub.NewClient(conn, user)
	s.hub.Register(client)

	// Send auth OK and channel list
	go s.sendInitialData(client)

	// Start read/write pumps
	go s.writePump(client)
	s.readPump(client)
}

func (s *Server) sendInitialData(client *Client) {
	// Get server info
	serverInfo, err := s.db.GetServerInfo()
	if err != nil {
		log.Printf("Failed to get server info: %v", err)
		return
	}
	if serverInfo == nil {
		serverInfo = &models.Server{
			ID:          "default",
			Hostname:    s.hostname,
			DisplayName: s.hostname,
		}
	}
	serverInfo.Hostname = s.hostname

	// Send auth OK
	client.SendEnvelope(protocol.TypeAuthOK, protocol.AuthOKMessage{
		User:   *client.User(),
		Server: *serverInfo,
	})

	// Send channel list
	s.sendChannelList(client)
}

func (s *Server) sendChannelList(client *Client) {
	categories, err := s.db.GetCategories()
	if err != nil {
		log.Printf("Failed to get categories: %v", err)
		return
	}

	var catWithChannels []protocol.CategoryWithChannels
	for _, cat := range categories {
		channels, err := s.db.GetChannels(cat.ID)
		if err != nil {
			log.Printf("Failed to get channels for category %s: %v", cat.ID, err)
			continue
		}

		// Filter channels based on access control
		var accessibleChannels []models.Channel
		for _, ch := range channels {
			if s.canAccessChannel(client.User(), &ch) {
				accessibleChannels = append(accessibleChannels, ch)
			}
		}

		catWithChannels = append(catWithChannels, protocol.CategoryWithChannels{
			Category: cat,
			Channels: accessibleChannels,
		})
	}

	client.SendEnvelope(protocol.TypeChannelList, protocol.ChannelListMessage{
		Categories: catWithChannels,
	})
}

// BroadcastChannelList sends updated channel list to all connected clients.
func (s *Server) BroadcastChannelList() {
	for _, client := range s.hub.GetClients() {
		s.sendChannelList(client)
	}
}

func (s *Server) canAccessChannel(user *models.User, channel *models.Channel) bool {
	// If no allow/deny lists, channel is accessible to all
	if len(channel.AllowList) == 0 && len(channel.DenyList) == 0 {
		return true
	}

	userGroups, _ := s.db.GetUserGroups(user.ID)

	// Check deny list first
	for _, denied := range channel.DenyList {
		if denied == user.ID {
			return false
		}
		for _, groupID := range userGroups {
			if denied == groupID {
				return false
			}
		}
	}

	// If there's an allow list, user must be on it
	if len(channel.AllowList) > 0 {
		for _, allowed := range channel.AllowList {
			if allowed == user.ID {
				return true
			}
			for _, groupID := range userGroups {
				if allowed == groupID {
					return true
				}
			}
		}
		return false
	}

	return true
}

func (s *Server) readPump(client *Client) {
	defer func() {
		s.hub.Unregister(client)
		client.Conn().Close()
	}()

	client.Conn().SetReadLimit(65536)
	client.Conn().SetReadDeadline(time.Now().Add(60 * time.Second))
	client.Conn().SetPongHandler(func(string) error {
		client.Conn().SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, message, err := client.Conn().ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error: %v", err)
			}
			break
		}

		s.handleMessage(client, message)
	}
}

func (s *Server) writePump(client *Client) {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		client.Conn().Close()
	}()

	for {
		select {
		case message, ok := <-client.SendChan():
			client.Conn().SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				client.Conn().WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if err := client.Conn().WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}

		case <-ticker.C:
			client.Conn().SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := client.Conn().WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (s *Server) handleMessage(client *Client, data []byte) {
	env, err := protocol.ParseEnvelope(data)
	if err != nil {
		client.SendError(protocol.ErrCodeInvalidMsg, "Invalid message format")
		return
	}

	switch env.Type {
	case protocol.TypeSubscribe:
		var msg protocol.SubscribeMessage
		if err := json.Unmarshal(env.Data, &msg); err != nil {
			client.SendError(protocol.ErrCodeInvalidMsg, "Invalid subscribe message")
			return
		}
		s.handleSubscribe(client, &msg)

	case protocol.TypeUnsubscribe:
		var msg protocol.UnsubscribeMessage
		if err := json.Unmarshal(env.Data, &msg); err != nil {
			client.SendError(protocol.ErrCodeInvalidMsg, "Invalid unsubscribe message")
			return
		}
		s.handleUnsubscribe(client, &msg)

	case protocol.TypeSendMessage:
		var msg protocol.SendMessageMessage
		if err := json.Unmarshal(env.Data, &msg); err != nil {
			client.SendError(protocol.ErrCodeInvalidMsg, "Invalid send_message")
			return
		}
		s.handleSendMessage(client, &msg)

	case protocol.TypeGetHistory:
		var msg protocol.GetHistoryMessage
		if err := json.Unmarshal(env.Data, &msg); err != nil {
			client.SendError(protocol.ErrCodeInvalidMsg, "Invalid get_history")
			return
		}
		s.handleGetHistory(client, &msg)

	default:
		client.SendError(protocol.ErrCodeInvalidMsg, "Unknown message type")
	}
}

func (s *Server) handleSubscribe(client *Client, msg *protocol.SubscribeMessage) {
	channel, err := s.db.GetChannel(msg.ChannelID)
	if err != nil || channel == nil {
		client.SendError(protocol.ErrCodeNotFound, "Channel not found")
		return
	}

	if !s.canAccessChannel(client.User(), channel) {
		client.SendError(protocol.ErrCodeForbidden, "Access denied")
		return
	}

	s.hub.Subscribe(client, msg.ChannelID)
	log.Printf("User %s subscribed to channel %s", client.User().LoginName, msg.ChannelID)
}

func (s *Server) handleUnsubscribe(client *Client, msg *protocol.UnsubscribeMessage) {
	s.hub.Unsubscribe(client, msg.ChannelID)
	log.Printf("User %s unsubscribed from channel %s", client.User().LoginName, msg.ChannelID)
}

func (s *Server) handleSendMessage(client *Client, msg *protocol.SendMessageMessage) {
	channel, err := s.db.GetChannel(msg.ChannelID)
	if err != nil || channel == nil {
		client.SendError(protocol.ErrCodeNotFound, "Channel not found")
		return
	}

	if !s.canAccessChannel(client.User(), channel) {
		client.SendError(protocol.ErrCodeForbidden, "Access denied")
		return
	}

	// Create and store the message
	chatMsg, err := s.db.CreateMessage(msg.ChannelID, client.User(), msg.Content)
	if err != nil {
		log.Printf("Failed to create message: %v", err)
		client.SendError(protocol.ErrCodeInternal, "Failed to save message")
		return
	}

	// Broadcast to all subscribers
	s.hub.Broadcast(msg.ChannelID, chatMsg, client.User())
}

func (s *Server) handleGetHistory(client *Client, msg *protocol.GetHistoryMessage) {
	channel, err := s.db.GetChannel(msg.ChannelID)
	if err != nil || channel == nil {
		client.SendError(protocol.ErrCodeNotFound, "Channel not found")
		return
	}

	if !s.canAccessChannel(client.User(), channel) {
		client.SendError(protocol.ErrCodeForbidden, "Access denied")
		return
	}

	limit := msg.Limit
	if limit <= 0 || limit > 100 {
		limit = 100
	}

	// Fetch one extra to determine if there are more messages
	dbMessages, err := s.db.GetMessagesWithAuthors(msg.ChannelID, limit+1, msg.Before)
	if err != nil {
		log.Printf("Failed to get messages: %v", err)
		client.SendError(protocol.ErrCodeInternal, "Failed to get messages")
		return
	}

	hasMore := len(dbMessages) > limit
	if hasMore {
		dbMessages = dbMessages[1:] // Remove the oldest (first after reversal)
	}

	// Convert to protocol format
	messages := make([]protocol.MessageWithAuthor, len(dbMessages))
	for i, m := range dbMessages {
		messages[i] = protocol.MessageWithAuthor{
			Message: m.Message,
			Author:  &m.Author,
		}
	}

	client.SendEnvelope(protocol.TypeHistory, protocol.HistoryMessage{
		ChannelID: msg.ChannelID,
		Messages:  messages,
		HasMore:   hasMore,
	})
}

// HandleIndex serves the main page.
func (s *Server) HandleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, "web/admin/index.html")
}
