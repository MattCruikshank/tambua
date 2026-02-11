package server

import (
	"encoding/json"
	"log"
	"sync"

	"github.com/MattCruikshank/tambua/internal/models"
	"github.com/MattCruikshank/tambua/internal/protocol"
	"github.com/gorilla/websocket"
)

// Client represents a connected WebSocket client.
type Client struct {
	hub     *Hub
	conn    *websocket.Conn
	user    *models.User
	send    chan []byte
	subs    map[string]bool // Subscribed channel IDs
	subsMu  sync.RWMutex
}

// Hub manages WebSocket connections and message broadcasting.
type Hub struct {
	clients    map[*Client]bool
	clientsMu  sync.RWMutex
	channels   map[string]map[*Client]bool // channelID -> clients
	channelsMu sync.RWMutex
	register   chan *Client
	unregister chan *Client
	broadcast  chan *channelMessage
}

type channelMessage struct {
	channelID string
	data      []byte
}

// NewHub creates a new Hub.
func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		channels:   make(map[string]map[*Client]bool),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		broadcast:  make(chan *channelMessage, 256),
	}
}

// Run starts the hub's main loop.
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.clientsMu.Lock()
			h.clients[client] = true
			h.clientsMu.Unlock()
			log.Printf("Client connected: %s", client.user.LoginName)

		case client := <-h.unregister:
			h.clientsMu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.clientsMu.Unlock()

			// Remove from all channel subscriptions
			client.subsMu.RLock()
			for channelID := range client.subs {
				h.unsubscribeFromChannel(client, channelID)
			}
			client.subsMu.RUnlock()
			log.Printf("Client disconnected: %s", client.user.LoginName)

		case msg := <-h.broadcast:
			h.channelsMu.RLock()
			clients := h.channels[msg.channelID]
			h.channelsMu.RUnlock()

			for client := range clients {
				select {
				case client.send <- msg.data:
				default:
					// Client buffer full, disconnect
					h.unregister <- client
				}
			}
		}
	}
}

// Subscribe subscribes a client to a channel.
func (h *Hub) Subscribe(client *Client, channelID string) {
	h.channelsMu.Lock()
	if h.channels[channelID] == nil {
		h.channels[channelID] = make(map[*Client]bool)
	}
	h.channels[channelID][client] = true
	h.channelsMu.Unlock()

	client.subsMu.Lock()
	client.subs[channelID] = true
	client.subsMu.Unlock()
}

// Unsubscribe unsubscribes a client from a channel.
func (h *Hub) Unsubscribe(client *Client, channelID string) {
	h.unsubscribeFromChannel(client, channelID)

	client.subsMu.Lock()
	delete(client.subs, channelID)
	client.subsMu.Unlock()
}

func (h *Hub) unsubscribeFromChannel(client *Client, channelID string) {
	h.channelsMu.Lock()
	if clients, ok := h.channels[channelID]; ok {
		delete(clients, client)
		if len(clients) == 0 {
			delete(h.channels, channelID)
		}
	}
	h.channelsMu.Unlock()
}

// Broadcast sends a message to all clients subscribed to a channel.
func (h *Hub) Broadcast(channelID string, msg *models.Message, author *models.User) {
	env, err := protocol.NewEnvelope(protocol.TypeMessage, protocol.MessageMessage{
		ChannelID: channelID,
		Message:   *msg,
		Author:    author,
	})
	if err != nil {
		log.Printf("Failed to create message envelope: %v", err)
		return
	}

	data, err := json.Marshal(env)
	if err != nil {
		log.Printf("Failed to marshal message: %v", err)
		return
	}

	h.broadcast <- &channelMessage{
		channelID: channelID,
		data:      data,
	}
}

// NewClient creates a new client for the hub.
func (h *Hub) NewClient(conn *websocket.Conn, user *models.User) *Client {
	return &Client{
		hub:  h,
		conn: conn,
		user: user,
		send: make(chan []byte, 256),
		subs: make(map[string]bool),
	}
}

// Register registers a client with the hub.
func (h *Hub) Register(client *Client) {
	h.register <- client
}

// Unregister unregisters a client from the hub.
func (h *Hub) Unregister(client *Client) {
	h.unregister <- client
}

// GetClients returns all connected clients.
func (h *Hub) GetClients() []*Client {
	h.clientsMu.RLock()
	defer h.clientsMu.RUnlock()
	clients := make([]*Client, 0, len(h.clients))
	for client := range h.clients {
		clients = append(clients, client)
	}
	return clients
}

// Send sends data to the client.
func (c *Client) Send(data []byte) {
	select {
	case c.send <- data:
	default:
		// Buffer full
	}
}

// SendEnvelope sends a protocol envelope to the client.
func (c *Client) SendEnvelope(msgType protocol.MessageType, data interface{}) error {
	env, err := protocol.NewEnvelope(msgType, data)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(env)
	if err != nil {
		return err
	}
	c.Send(raw)
	return nil
}

// SendError sends an error message to the client.
func (c *Client) SendError(code, message string) {
	c.SendEnvelope(protocol.TypeError, protocol.ErrorMessage{
		Code:    code,
		Message: message,
	})
}

// User returns the client's user.
func (c *Client) User() *models.User {
	return c.user
}

// Conn returns the client's WebSocket connection.
func (c *Client) Conn() *websocket.Conn {
	return c.conn
}

// SendChan returns the client's send channel.
func (c *Client) SendChan() <-chan []byte {
	return c.send
}
