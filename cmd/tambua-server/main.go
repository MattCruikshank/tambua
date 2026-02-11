package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/MattCruikshank/tambua/internal/auth"
	"github.com/MattCruikshank/tambua/internal/db"
	"github.com/MattCruikshank/tambua/server"
	"tailscale.com/tsnet"
)

func main() {
	// Configuration
	hostname := "tambua-server"
	if h := os.Getenv("TAMBUA_HOSTNAME"); h != "" {
		hostname = h
	}

	configDir := filepath.Join(os.Getenv("HOME"), ".config", "tambua")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		log.Fatalf("Failed to create config directory: %v", err)
	}

	// Initialize tsnet server
	tsServer := &tsnet.Server{
		Hostname: hostname,
		Dir:      filepath.Join(configDir, "server-state"),
	}

	// Start the server
	log.Printf("Starting Tambua server as %s...", hostname)
	ln, err := tsServer.ListenTLS("tcp", ":443")
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}
	defer ln.Close()

	// Get the local client for auth
	lc, err := tsServer.LocalClient()
	if err != nil {
		log.Fatalf("Failed to get local client: %v", err)
	}

	// Initialize database
	dbPath := filepath.Join(configDir, "server.db")
	database, err := db.NewServerDB(dbPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	// Initialize components
	authenticator := auth.NewAuthenticator(lc)
	hub := server.NewHub()
	go hub.Run()

	srv := server.NewServer(hub, database, authenticator, hostname)
	admin := server.NewAdminHandler(database, authenticator)

	// Set up routes
	mux := http.NewServeMux()

	// WebSocket endpoint
	mux.HandleFunc("/ws", srv.HandleWebSocket)

	// Admin API endpoints
	mux.HandleFunc("/api/server", admin.HandleServerInfo)
	mux.HandleFunc("/api/categories", admin.HandleCategories)
	mux.HandleFunc("/api/categories/{id}", admin.HandleCategory)
	mux.HandleFunc("/api/channels", admin.HandleChannels)
	mux.HandleFunc("/api/channels/{id}", admin.HandleChannel)
	mux.HandleFunc("/api/messages", admin.HandleMessages)
	mux.HandleFunc("/api/groups", admin.HandleGroups)
	mux.HandleFunc("/api/groups/{id}", admin.HandleGroup)
	mux.HandleFunc("/api/groups/{id}/members", admin.HandleGroupMembers)
	mux.HandleFunc("/api/groups/{id}/members/{user_id}", admin.HandleGroupMember)

	// Static files
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("web/admin"))))
	mux.HandleFunc("/", srv.HandleIndex)

	// Apply auth middleware to admin routes
	handler := authenticator.Middleware(mux)

	httpServer := &http.Server{
		Handler: handler,
	}

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh

		log.Println("Shutting down...")
		httpServer.Shutdown(context.Background())
		tsServer.Close()
	}()

	// Get the actual tailnet domain
	url := hostname
	if domains := tsServer.CertDomains(); len(domains) > 0 {
		url = domains[0]
	}
	log.Printf("Tambua server running at https://%s", url)
	if err := httpServer.Serve(ln); err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}
}
