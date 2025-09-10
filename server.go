package main

import (
	"context"
	_ "embed"
	"fmt"
	"log"
	"net/http"
	"strings"

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

// handleSubscribe handles SSE subscriptions for single or multiple topics
func (s *Server) handleSubscribe(w http.ResponseWriter, r *http.Request) {
	log.Printf("New SSE connection request from %s", r.RemoteAddr)

	// Get topics parameter (supports both single and comma-separated multiple topics)
	topicsParam := r.URL.Query().Get("topics")
	if topicsParam == "" {
		log.Printf("Rejected connection from %s: missing topics parameter", r.RemoteAddr)
		http.Error(w, "topics parameter is required", http.StatusBadRequest)
		return
	}

	// Parse comma-separated topics and sanitize to lowercase
	topics := strings.Split(topicsParam, ",")
	for i, topic := range topics {
		topics[i] = strings.ToLower(strings.TrimSpace(topic))
	}

	// Validate all UUIDs
	for _, topic := range topics {
		if topic == "" {
			log.Printf("Rejected connection from %s: empty topic in list", r.RemoteAddr)
			http.Error(w, "empty topic not allowed", http.StatusBadRequest)
			return
		}
		if _, err := uuid.Parse(topic); err != nil {
			log.Printf("Rejected connection from %s: invalid UUID format: %s", r.RemoteAddr, topic)
			http.Error(w, fmt.Sprintf("invalid UUID format: %s", topic), http.StatusBadRequest)
			return
		}
	}

	log.Printf("Valid subscription request from %s for %d topic(s): %v", r.RemoteAddr, len(topics), topics)

	// Set headers for SSE
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Subscribe to all topics
	messageChannels := make([]chan string, len(topics))
	for i, topic := range topics {
		messageChannels[i] = s.broker.Subscribe(topic)
	}

	// Cleanup function
	defer func() {
		for i, topic := range topics {
			s.broker.Unsubscribe(topic, messageChannels[i])
		}
	}()

	// Get context from request for client disconnect
	ctx := r.Context()

	// Create a multiplexed channel to receive messages from all subscriptions
	multiplexCh := make(chan struct {
		topic   string
		message string
	}, 100)

	// Start goroutines to forward messages from each topic
	for i, ch := range messageChannels {
		go func(topicName string, messageCh chan string) {
			for msg := range messageCh {
				select {
				case multiplexCh <- struct {
					topic   string
					message string
				}{topicName, msg}:
				case <-ctx.Done():
					return
				}
			}
		}(topics[i], ch)
	}

	// Send messages to client (always in JSON format)
	for {
		select {
		case msgData := <-multiplexCh:
			// Always include topic info in JSON format
			fmt.Fprintf(w, "data: {\"topic\":\"%s\",\"message\":\"%s\"}\n\n", msgData.topic, msgData.message)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		case <-ctx.Done():
			return
		}
	}
}
