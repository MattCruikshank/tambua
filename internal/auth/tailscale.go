package auth

import (
	"context"
	"fmt"
	"net/http"

	"github.com/MattCruikshank/tambua/internal/models"
	"tailscale.com/client/tailscale"
)

// contextKey is a custom type for context keys.
type contextKey string

const userContextKey contextKey = "user"

// Authenticator extracts Tailscale identity from requests.
type Authenticator struct {
	lc *tailscale.LocalClient
}

// NewAuthenticator creates a new authenticator.
func NewAuthenticator(lc *tailscale.LocalClient) *Authenticator {
	return &Authenticator{lc: lc}
}

// GetUser extracts the Tailscale user from an HTTP request.
func (a *Authenticator) GetUser(ctx context.Context, r *http.Request) (*models.User, error) {
	who, err := a.lc.WhoIs(ctx, r.RemoteAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to get caller identity: %w", err)
	}

	if who.UserProfile == nil {
		return nil, fmt.Errorf("no user profile for caller")
	}

	return &models.User{
		ID:          fmt.Sprintf("%d", who.UserProfile.ID),
		LoginName:   who.UserProfile.LoginName,
		DisplayName: who.UserProfile.DisplayName,
		ProfilePic:  who.UserProfile.ProfilePicURL,
	}, nil
}

// Middleware wraps an HTTP handler and adds the user to the context.
func (a *Authenticator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, err := a.GetUser(r.Context(), r)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), userContextKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// UserFromContext retrieves the user from the request context.
func UserFromContext(ctx context.Context) *models.User {
	user, ok := ctx.Value(userContextKey).(*models.User)
	if !ok {
		return nil
	}
	return user
}
