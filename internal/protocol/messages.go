package protocol

import (
	"encoding/json"

	"github.com/MattCruikshank/tambua/internal/models"
)

// MessageType identifies the type of WebSocket message.
type MessageType string

const (
	// Client -> Server
	TypeAuth        MessageType = "auth"
	TypeSubscribe   MessageType = "subscribe"
	TypeUnsubscribe MessageType = "unsubscribe"
	TypeSendMessage MessageType = "send_message"
	TypeGetHistory  MessageType = "get_history"

	// Server -> Client
	TypeAuthOK      MessageType = "auth_ok"
	TypeChannelList MessageType = "channel_list"
	TypeMessage     MessageType = "message"
	TypeHistory     MessageType = "history"
	TypeError       MessageType = "error"
)

// Envelope wraps all WebSocket messages with a type field.
type Envelope struct {
	Type MessageType     `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

// AuthMessage is sent by the client to authenticate.
type AuthMessage struct {
	Token string `json:"token"` // Tailscale identity token (for future use)
}

// SubscribeMessage is sent by the client to subscribe to a channel.
type SubscribeMessage struct {
	ChannelID string `json:"channel_id"`
}

// UnsubscribeMessage is sent by the client to unsubscribe from a channel.
type UnsubscribeMessage struct {
	ChannelID string `json:"channel_id"`
}

// SendMessageMessage is sent by the client to send a chat message.
type SendMessageMessage struct {
	ChannelID string       `json:"channel_id"`
	Content   string       `json:"content"`
	Sender    *models.User `json:"sender,omitempty"` // Asserted browser user identity (from tambua client)
}

// GetHistoryMessage is sent by the client to request message history.
type GetHistoryMessage struct {
	ChannelID string `json:"channel_id"`
	Before    string `json:"before,omitempty"` // Message ID to fetch messages before (for pagination)
	Limit     int    `json:"limit,omitempty"`  // Max messages to return (default 100)
}

// AuthOKMessage is sent by the server after successful authentication.
type AuthOKMessage struct {
	User   models.User   `json:"user"`
	Server models.Server `json:"server"`
}

// CategoryWithChannels includes a category and its channels.
type CategoryWithChannels struct {
	Category models.Category  `json:"category"`
	Channels []models.Channel `json:"channels"`
}

// ChannelListMessage is sent by the server with available channels.
type ChannelListMessage struct {
	Categories []CategoryWithChannels `json:"categories"`
}

// MessageMessage is sent by the server when a new message arrives.
type MessageMessage struct {
	ChannelID string         `json:"channel_id"`
	Message   models.Message `json:"message"`
	Author    *models.User   `json:"author,omitempty"`
}

// MessageWithAuthor pairs a message with its author info.
type MessageWithAuthor struct {
	Message models.Message `json:"message"`
	Author  *models.User   `json:"author,omitempty"`
}

// HistoryMessage is sent by the server with message history.
type HistoryMessage struct {
	ChannelID string              `json:"channel_id"`
	Messages  []MessageWithAuthor `json:"messages"`
	HasMore   bool                `json:"has_more"` // True if more older messages exist
}

// ErrorMessage is sent by the server when an error occurs.
type ErrorMessage struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Error codes
const (
	ErrCodeUnauthorized = "unauthorized"
	ErrCodeForbidden    = "forbidden"
	ErrCodeNotFound     = "not_found"
	ErrCodeInvalidMsg   = "invalid_message"
	ErrCodeInternal     = "internal_error"
)

// NewEnvelope creates an envelope with the given type and data.
func NewEnvelope(msgType MessageType, data interface{}) (*Envelope, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	return &Envelope{
		Type: msgType,
		Data: raw,
	}, nil
}

// ParseEnvelope parses a JSON message into an envelope.
func ParseEnvelope(data []byte) (*Envelope, error) {
	var env Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, err
	}
	return &env, nil
}
