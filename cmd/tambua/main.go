package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/MattCruikshank/tambua/client"
	"github.com/MattCruikshank/tambua/internal/db"
	"tailscale.com/tsnet"
)

func main() {
	// Configuration
	hostname := "tambua"
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
		Dir:      filepath.Join(configDir, "client-state"),
	}

	// Start the server
	log.Printf("Starting Tambua client as %s...", hostname)
	ln, err := tsServer.ListenTLS("tcp", ":443")
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}
	defer ln.Close()

	// Initialize database
	dbPath := filepath.Join(configDir, "client.db")
	database, err := db.NewClientDB(dbPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	// Initialize aggregator and handlers
	aggregator := client.NewAggregator(database, tsServer)
	handler := client.NewClientHandler(database, aggregator)

	// Set up routes
	mux := http.NewServeMux()

	// WebSocket for UI
	mux.HandleFunc("/ws", handler.HandleWebSocket)

	// API endpoints
	mux.HandleFunc("/api/servers", handler.HandleServers)
	mux.HandleFunc("/api/preferences", handler.HandlePreferences)

	// Static files
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("web/client"))))
	mux.HandleFunc("/", handler.HandleIndex)

	httpServer := &http.Server{
		Handler: mux,
	}

	// Auto-connect to enrolled servers
	go func() {
		servers, err := database.GetEnrolledServers()
		if err != nil {
			log.Printf("Failed to load enrolled servers: %v", err)
			return
		}
		for _, srv := range servers {
			if err := aggregator.Connect(context.Background(), srv.Hostname); err != nil {
				log.Printf("Failed to connect to %s: %v", srv.Hostname, err)
			}
		}
	}()

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
	log.Printf("Tambua client running at https://%s", url)
	if err := httpServer.Serve(ln); err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}
}
