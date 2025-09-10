package main

import (
	"context"
	_ "embed"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

//go:embed static/index.html
var indexHTML []byte

// Server encapsulates HTTP server and its dependencies
type Server struct {
	broker *Broker
	config *Config
	server *http.Server
}

// NewServer creates a new server with dependencies injected
func NewServer(broker *Broker, config *Config) *Server {
	s := &Server{
		broker: broker,
		config: config,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleHome)
	mux.HandleFunc("/subscribe", s.handleSubscribe)

	s.server = &http.Server{
		Addr:    config.ServerAddress,
		Handler: mux,
	}

	return s
}

// Start starts the HTTP server
func (s *Server) Start() error {
	log.Printf("Server starting on %s", s.config.ServerAddress)
	return s.server.ListenAndServe()
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(ctx context.Context) error {
	log.Println("Shutting down server...")
	return s.server.Shutdown(ctx)
}

// handleHome serves the HTML interface
func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	w.Write(indexHTML)
}

// handleSubscribe handles SSE subscriptions
func (s *Server) handleSubscribe(w http.ResponseWriter, r *http.Request) {
	log.Printf("New SSE connection request from %s", r.RemoteAddr)

	topicStr := r.URL.Query().Get("topic")
	if topicStr == "" {
		log.Printf("Rejected connection from %s: missing topic parameter", r.RemoteAddr)
		http.Error(w, "topic parameter is required", http.StatusBadRequest)
		return
	}

	// Validate UUID
	if _, err := uuid.Parse(topicStr); err != nil {
		log.Printf("Rejected connection from %s: invalid UUID format: %s", r.RemoteAddr, topicStr)
		http.Error(w, "invalid UUID format", http.StatusBadRequest)
		return
	}

	log.Printf("Valid subscription request from %s for topic %s", r.RemoteAddr, topicStr)

	// Set headers for SSE
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Subscribe to messages
	messageCh := s.broker.Subscribe(topicStr)
	defer s.broker.Unsubscribe(topicStr, messageCh)

	// Get context from request for client disconnect
	ctx := r.Context()

	// Send messages to client
	for {
		select {
		case msg := <-messageCh:
			fmt.Fprintf(w, "data: %s\n\n", msg)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		case <-ctx.Done():
			return
		}
	}
}
