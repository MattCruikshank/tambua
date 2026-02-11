package client

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sync"

	"github.com/MattCruikshank/tambua/internal/db"
	"github.com/MattCruikshank/tambua/internal/protocol"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// ClientHandler handles HTTP requests for the client UI.
type ClientHandler struct {
	db         *db.ClientDB
	aggregator *Aggregator
	uiClients  map[*websocket.Conn]bool
	uiMu       sync.RWMutex
	broadcast  chan []byte
}

// NewClientHandler creates a new client handler.
func NewClientHandler(database *db.ClientDB, aggregator *Aggregator) *ClientHandler {
	h := &ClientHandler{
		db:         database,
		aggregator: aggregator,
		uiClients:  make(map[*websocket.Conn]bool),
		broadcast:  make(chan []byte, 256),
	}

	// Set up message handler to forward to UI
	aggregator.SetMessageHandler(func(serverID string, msg *protocol.MessageMessage) {
		data, _ := json.Marshal(map[string]interface{}{
			"type":      "message",
			"server_id": serverID,
			"message":   msg,
		})
		h.broadcastToUI(data)
	})

	// Set up history handler to forward to UI
	aggregator.SetHistoryHandler(func(serverID string, msg *protocol.HistoryMessage) {
		data, _ := json.Marshal(map[string]interface{}{
			"type":      "history",
			"server_id": serverID,
			"history":   msg,
		})
		h.broadcastToUI(data)
	})

	aggregator.SetUpdateHandler(func(serverID string) {
		conn := aggregator.GetConnection(serverID)
		if conn == nil {
			return
		}
		data, _ := json.Marshal(map[string]interface{}{
			"type":       "server_update",
			"server_id":  serverID,
			"server":     conn.GetServerInfo(),
			"categories": conn.GetCategories(),
		})
		h.broadcastToUI(data)
	})

	go h.runBroadcast()
	return h
}

func (h *ClientHandler) runBroadcast() {
	for data := range h.broadcast {
		h.uiMu.RLock()
		for conn := range h.uiClients {
			err := conn.WriteMessage(websocket.TextMessage, data)
			if err != nil {
				conn.Close()
				delete(h.uiClients, conn)
			}
		}
		h.uiMu.RUnlock()
	}
}

func (h *ClientHandler) broadcastToUI(data []byte) {
	select {
	case h.broadcast <- data:
	default:
		// Drop if buffer full
	}
}

// HandleIndex serves the main client UI.
func (h *ClientHandler) HandleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, "web/client/index.html")
}

// HandleWebSocket handles WebSocket connections from the browser UI.
func (h *ClientHandler) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}

	h.uiMu.Lock()
	h.uiClients[conn] = true
	h.uiMu.Unlock()

	// Send initial state
	h.sendInitialState(conn)

	// Handle incoming messages
	go h.handleUIMessages(conn)
}

func (h *ClientHandler) sendInitialState(conn *websocket.Conn) {
	// Send enrolled servers
	servers, _ := h.db.GetEnrolledServers()
	conn.WriteJSON(map[string]interface{}{
		"type":    "servers",
		"servers": servers,
	})

	// Send current connection states
	for _, sc := range h.aggregator.GetConnections() {
		conn.WriteJSON(map[string]interface{}{
			"type":       "server_update",
			"server_id":  sc.GetEnrolledServer().ID,
			"server":     sc.GetServerInfo(),
			"categories": sc.GetCategories(),
			"user":       sc.GetUser(),
		})
	}
}

func (h *ClientHandler) handleUIMessages(conn *websocket.Conn) {
	defer func() {
		h.uiMu.Lock()
		delete(h.uiClients, conn)
		h.uiMu.Unlock()
		conn.Close()
	}()

	for {
		var msg map[string]interface{}
		if err := conn.ReadJSON(&msg); err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("UI WebSocket error: %v", err)
			}
			return
		}

		h.handleUIMessage(conn, msg)
	}
}

func (h *ClientHandler) handleUIMessage(conn *websocket.Conn, msg map[string]interface{}) {
	msgType, _ := msg["type"].(string)

	switch msgType {
	case "connect":
		hostname, _ := msg["hostname"].(string)
		if hostname == "" {
			conn.WriteJSON(map[string]interface{}{
				"type":  "error",
				"error": "Missing hostname",
			})
			return
		}
		if err := h.aggregator.Connect(context.Background(), hostname); err != nil {
			conn.WriteJSON(map[string]interface{}{
				"type":  "error",
				"error": err.Error(),
			})
			return
		}
		// Refresh server list
		servers, _ := h.db.GetEnrolledServers()
		h.broadcastToUI(mustMarshal(map[string]interface{}{
			"type":    "servers",
			"servers": servers,
		}))

	case "disconnect":
		serverID, _ := msg["server_id"].(string)
		h.aggregator.Disconnect(serverID)

	case "subscribe":
		serverID, _ := msg["server_id"].(string)
		channelID, _ := msg["channel_id"].(string)
		if err := h.aggregator.Subscribe(serverID, channelID); err != nil {
			conn.WriteJSON(map[string]interface{}{
				"type":  "error",
				"error": err.Error(),
			})
		}

	case "unsubscribe":
		serverID, _ := msg["server_id"].(string)
		channelID, _ := msg["channel_id"].(string)
		h.aggregator.Unsubscribe(serverID, channelID)

	case "send_message":
		serverID, _ := msg["server_id"].(string)
		channelID, _ := msg["channel_id"].(string)
		content, _ := msg["content"].(string)
		if err := h.aggregator.SendMessage(serverID, channelID, content); err != nil {
			conn.WriteJSON(map[string]interface{}{
				"type":  "error",
				"error": err.Error(),
			})
		}

	case "get_history":
		serverID, _ := msg["server_id"].(string)
		channelID, _ := msg["channel_id"].(string)
		beforeID, _ := msg["before"].(string)
		if err := h.aggregator.GetHistory(serverID, channelID, beforeID); err != nil {
			conn.WriteJSON(map[string]interface{}{
				"type":  "error",
				"error": err.Error(),
			})
		}
	}
}

func mustMarshal(v interface{}) []byte {
	data, _ := json.Marshal(v)
	return data
}

// HandleServers returns enrolled servers.
func (h *ClientHandler) HandleServers(w http.ResponseWriter, r *http.Request) {
	servers, err := h.db.GetEnrolledServers()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(servers)
}

// HandlePreferences handles preference operations.
func (h *ClientHandler) HandlePreferences(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		key := r.URL.Query().Get("key")
		if key == "" {
			http.Error(w, "Missing key", http.StatusBadRequest)
			return
		}
		value, err := h.db.GetPreference(key)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"value": value})

	case http.MethodPut:
		var req struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}
		if err := h.db.SetPreference(req.Key, req.Value); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}
