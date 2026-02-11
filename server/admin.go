package server

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/MattCruikshank/tambua/internal/auth"
	"github.com/MattCruikshank/tambua/internal/db"
	"github.com/MattCruikshank/tambua/internal/models"
)

// AdminHandler handles admin API requests.
type AdminHandler struct {
	db   *db.ServerDB
	auth *auth.Authenticator
}

// NewAdminHandler creates a new admin handler.
func NewAdminHandler(database *db.ServerDB, authenticator *auth.Authenticator) *AdminHandler {
	return &AdminHandler{
		db:   database,
		auth: authenticator,
	}
}

func (a *AdminHandler) writeJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func (a *AdminHandler) writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}

// HandleCategories handles category CRUD operations.
func (a *AdminHandler) HandleCategories(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		categories, err := a.db.GetCategories()
		if err != nil {
			a.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		a.writeJSON(w, categories)

	case http.MethodPost:
		var req struct {
			Name     string `json:"name"`
			Position int    `json:"position"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			a.writeError(w, http.StatusBadRequest, "Invalid request body")
			return
		}
		cat, err := a.db.CreateCategory(req.Name, req.Position)
		if err != nil {
			a.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusCreated)
		a.writeJSON(w, cat)

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// HandleCategory handles single category operations.
func (a *AdminHandler) HandleCategory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		a.writeError(w, http.StatusBadRequest, "Missing category ID")
		return
	}

	switch r.Method {
	case http.MethodPut:
		var cat models.Category
		if err := json.NewDecoder(r.Body).Decode(&cat); err != nil {
			a.writeError(w, http.StatusBadRequest, "Invalid request body")
			return
		}
		cat.ID = id
		if err := a.db.UpdateCategory(&cat); err != nil {
			a.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		a.writeJSON(w, cat)

	case http.MethodDelete:
		if err := a.db.DeleteCategory(id); err != nil {
			a.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// HandleChannels handles channel CRUD operations.
func (a *AdminHandler) HandleChannels(w http.ResponseWriter, r *http.Request) {
	categoryID := r.URL.Query().Get("category_id")

	switch r.Method {
	case http.MethodGet:
		if categoryID == "" {
			a.writeError(w, http.StatusBadRequest, "Missing category_id")
			return
		}
		channels, err := a.db.GetChannels(categoryID)
		if err != nil {
			a.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		a.writeJSON(w, channels)

	case http.MethodPost:
		var req struct {
			Name       string `json:"name"`
			CategoryID string `json:"category_id"`
			Position   int    `json:"position"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			a.writeError(w, http.StatusBadRequest, "Invalid request body")
			return
		}
		ch, err := a.db.CreateChannel(req.Name, req.CategoryID, req.Position)
		if err != nil {
			a.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusCreated)
		a.writeJSON(w, ch)

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// HandleChannel handles single channel operations.
func (a *AdminHandler) HandleChannel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		a.writeError(w, http.StatusBadRequest, "Missing channel ID")
		return
	}

	switch r.Method {
	case http.MethodGet:
		ch, err := a.db.GetChannel(id)
		if err != nil {
			a.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if ch == nil {
			a.writeError(w, http.StatusNotFound, "Channel not found")
			return
		}
		a.writeJSON(w, ch)

	case http.MethodPut:
		var ch models.Channel
		if err := json.NewDecoder(r.Body).Decode(&ch); err != nil {
			a.writeError(w, http.StatusBadRequest, "Invalid request body")
			return
		}
		ch.ID = id
		if err := a.db.UpdateChannel(&ch); err != nil {
			a.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		a.writeJSON(w, ch)

	case http.MethodDelete:
		if err := a.db.DeleteChannel(id); err != nil {
			a.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// HandleMessages handles message retrieval.
func (a *AdminHandler) HandleMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	channelID := r.URL.Query().Get("channel_id")
	if channelID == "" {
		a.writeError(w, http.StatusBadRequest, "Missing channel_id")
		return
	}

	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}

	messages, err := a.db.GetMessages(channelID, limit, nil)
	if err != nil {
		a.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.writeJSON(w, messages)
}

// HandleGroups handles group CRUD operations.
func (a *AdminHandler) HandleGroups(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		groups, err := a.db.GetGroups()
		if err != nil {
			a.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		a.writeJSON(w, groups)

	case http.MethodPost:
		var req struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			a.writeError(w, http.StatusBadRequest, "Invalid request body")
			return
		}
		group, err := a.db.CreateGroup(req.Name)
		if err != nil {
			a.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusCreated)
		a.writeJSON(w, group)

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// HandleGroup handles single group operations.
func (a *AdminHandler) HandleGroup(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		a.writeError(w, http.StatusBadRequest, "Missing group ID")
		return
	}

	switch r.Method {
	case http.MethodDelete:
		if err := a.db.DeleteGroup(id); err != nil {
			a.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// HandleGroupMembers handles group member operations.
func (a *AdminHandler) HandleGroupMembers(w http.ResponseWriter, r *http.Request) {
	groupID := r.PathValue("id")
	if groupID == "" {
		a.writeError(w, http.StatusBadRequest, "Missing group ID")
		return
	}

	switch r.Method {
	case http.MethodGet:
		members, err := a.db.GetGroupMembers(groupID)
		if err != nil {
			a.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		a.writeJSON(w, members)

	case http.MethodPost:
		var req struct {
			UserID string `json:"user_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			a.writeError(w, http.StatusBadRequest, "Invalid request body")
			return
		}
		if err := a.db.AddGroupMember(groupID, req.UserID); err != nil {
			a.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusCreated)

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// HandleGroupMember handles single group member operations.
func (a *AdminHandler) HandleGroupMember(w http.ResponseWriter, r *http.Request) {
	groupID := r.PathValue("id")
	userID := r.PathValue("user_id")
	if groupID == "" || userID == "" {
		a.writeError(w, http.StatusBadRequest, "Missing group ID or user ID")
		return
	}

	switch r.Method {
	case http.MethodDelete:
		if err := a.db.RemoveGroupMember(groupID, userID); err != nil {
			a.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// HandleServerInfo handles server info operations.
func (a *AdminHandler) HandleServerInfo(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		info, err := a.db.GetServerInfo()
		if err != nil {
			a.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if info == nil {
			info = &models.Server{}
		}
		a.writeJSON(w, info)

	case http.MethodPut:
		var info models.Server
		if err := json.NewDecoder(r.Body).Decode(&info); err != nil {
			a.writeError(w, http.StatusBadRequest, "Invalid request body")
			return
		}
		if info.ID == "" {
			info.ID = "default"
		}
		if err := a.db.SetServerInfo(&info); err != nil {
			a.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		a.writeJSON(w, info)

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}
