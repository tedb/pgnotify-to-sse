package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// Config holds application configuration
type Config struct {
	DatabaseURL                  string
	ServerAddress                string
	ListenerMinReconnectInterval time.Duration
	ListenerMaxReconnectInterval time.Duration
}

// NewConfig creates a new configuration with defaults
func NewConfig() *Config {
	return &Config{
		DatabaseURL:                  getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost/postgres?sslmode=disable"),
		ServerAddress:                getEnv("SERVER_ADDRESS", ":8080"),
		ListenerMinReconnectInterval: 10 * time.Second,
		ListenerMaxReconnectInterval: time.Minute,
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

type Broker struct {
	clients    map[string]map[chan string]struct{} // topic -> channels
	listening  map[string]struct{}                 // topics we're already listening to
	mu         sync.RWMutex
	db         *sql.DB
	listenerCh chan *pq.Notification
	listener   *pq.Listener
}

func NewBroker(db *sql.DB, listener *pq.Listener) *Broker {
	log.Println("Creating new broker instance")
	return &Broker{
		clients:    make(map[string]map[chan string]struct{}),
		listening:  make(map[string]struct{}),
		db:         db,
		listenerCh: make(chan *pq.Notification, 100),
		listener:   listener,
	}
}

func (b *Broker) Subscribe(topic string) chan string {
	log.Printf("Subscribing to topic: %s", topic)
	ch := make(chan string, 100) // Increased buffer to handle rapid notifications
	b.mu.Lock()
	if _, exists := b.clients[topic]; !exists {
		log.Printf("First subscriber for topic %s, creating client map", topic)
		b.clients[topic] = make(map[chan string]struct{})
	} else {
		log.Printf("Adding subscriber to existing topic %s", topic)
	}

	// Check if we need to start listening
	if _, isListening := b.listening[topic]; !isListening {
		// Start listening on Postgres for this topic
		if err := b.listener.Listen(topic); err != nil {
			log.Printf("Error subscribing to topic %s: %v", topic, err)
		} else {
			log.Printf("Successfully executed LISTEN for topic %s", topic)
			b.listening[topic] = struct{}{}
		}
	}
	b.clients[topic][ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

func (b *Broker) Unsubscribe(topic string, ch chan string) {
	log.Printf("Unsubscribing from topic: %s", topic)

	// Close channel first to prevent deadlock
	close(ch)

	b.mu.Lock()
	defer b.mu.Unlock()

	if clients, exists := b.clients[topic]; exists {
		delete(clients, ch)
		log.Printf("Removed subscriber from topic %s, remaining subscribers: %d", topic, len(clients))
		if len(clients) == 0 {
			log.Printf("No more subscribers for topic %s, cleaning up", topic)
			// Stop listening on Postgres when no more clients
			// During shutdown, the listener might already be closed
			// Only try to unlisten if we're still listening to this topic
			if _, stillListening := b.listening[topic]; stillListening {
				if err := b.listener.Unlisten(topic); err != nil {
					if strings.Contains(err.Error(), "Listener has been closed") {
						// This is expected during shutdown
						log.Printf("Listener already closed for topic %s", topic)
					} else {
						log.Printf("Error unsubscribing from topic %s: %v", topic, err)
					}
				} else {
					log.Printf("Successfully executed UNLISTEN for topic %s", topic)
				}
				delete(b.listening, topic)
			}
			delete(b.clients, topic)
		}
	} else {
		log.Printf("No subscribers found for topic %s during unsubscribe", topic)
	}
}

func (b *Broker) Broadcast(topic string) {
	log.Printf("Broadcasting message to topic: %s", topic)

	// Create a copy of channels to avoid race condition
	b.mu.RLock()
	topicClients, exists := b.clients[topic]
	if !exists || len(topicClients) == 0 {
		b.mu.RUnlock()
		log.Printf("No subscribers found for topic %s", topic)
		return
	}

	// Copy channels to avoid holding lock during broadcast
	channels := make([]chan string, 0, len(topicClients))
	for ch := range topicClients {
		channels = append(channels, ch)
	}
	clientCount := len(channels)
	b.mu.RUnlock()

	log.Printf("Found %d subscribers for topic %s", clientCount, topic)
	successCount := 0
	droppedCount := 0

	for _, ch := range channels {
		select {
		case ch <- "yo":
			successCount++
		default:
			droppedCount++
			log.Printf("Channel full for a subscriber of topic %s, skipping (dropped: %d)", topic, droppedCount)
		}
	}

	if droppedCount > 0 {
		log.Printf("WARNING: Dropped %d messages for topic %s due to full channels", droppedCount, topic)
	}
	log.Printf("Successfully sent message to %d/%d subscribers for topic %s (dropped: %d)", successCount, clientCount, topic, droppedCount)
}

func (b *Broker) ListenForNotifications() {
	log.Println("Starting notification listener")
	for notification := range b.listenerCh {
		if notification != nil {
			log.Printf("Received notification on channel: %s, payload: %s", notification.Channel, notification.Extra)
			b.Broadcast(notification.Channel)
		} else {
			log.Println("Received nil notification")
		}
	}
	log.Println("Notification listener stopped")
}

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

	html := `
<!DOCTYPE html>
<html>
<head>
    <title>Postgres Notification Subscriber</title>
    <style>
        body {
            font-family: Arial, sans-serif;
            max-width: 800px;
            margin: 20px auto;
            padding: 0 20px;
        }
        .container {
            background-color: #f5f5f5;
            border-radius: 5px;
            padding: 20px;
            margin-top: 20px;
        }
        #messages {
            background-color: white;
            border: 1px solid #ddd;
            border-radius: 3px;
            padding: 10px;
            margin-top: 10px;
            min-height: 100px;
            max-height: 300px;
            overflow-y: auto;
        }
        .message {
            padding: 5px;
            border-bottom: 1px solid #eee;
        }
        input[type="text"] {
            padding: 5px;
            width: 300px;
            margin-right: 10px;
        }
        button {
            padding: 5px 15px;
            background-color: #4CAF50;
            color: white;
            border: none;
            border-radius: 3px;
            cursor: pointer;
        }
        button:hover {
            background-color: #45a049;
        }
        .status {
            margin-top: 10px;
            font-style: italic;
        }
    </style>
</head>
<body>
    <h1>Postgres Notification Subscriber</h1>
    <div class="container">
        <div>
            <input type="text" id="uuid" placeholder="Enter UUID to subscribe to...">
            <button onclick="subscribe()">Subscribe</button>
        </div>
        <div class="status" id="status">Not connected</div>
        <h3>Messages:</h3>
        <div id="messages"></div>
    </div>

    <script>
        let eventSource = null;

        function subscribe() {
            if (eventSource) {
                eventSource.close();
            }

            const uuid = document.getElementById('uuid').value.trim();
            if (!uuid) {
                alert('Please enter a UUID');
                return;
            }

            const statusEl = document.getElementById('status');
            const messagesEl = document.getElementById('messages');

            try {
                eventSource = new EventSource('/subscribe?topic=' + encodeURIComponent(uuid));
                
                statusEl.textContent = 'Connected and listening for messages...';
                
                eventSource.onmessage = function(event) {
                    const messageEl = document.createElement('div');
                    messageEl.className = 'message';
                    messageEl.textContent = 'Received: ' + event.data;
                    messagesEl.insertBefore(messageEl, messagesEl.firstChild);
                };

                eventSource.onerror = function() {
                    statusEl.textContent = 'Connection error. Reconnecting...';
                };
            } catch (err) {
                statusEl.textContent = 'Error: ' + err.message;
            }
        }
    </script>
</body>
</html>
`
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(html))
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

// App encapsulates the entire application
type App struct {
	config   *Config
	db       *sql.DB
	listener *pq.Listener
	broker   *Broker
	server   *Server
}

// NewApp creates a new application with all dependencies
func NewApp(config *Config) (*App, error) {
	app := &App{config: config}

	if err := app.initDatabase(); err != nil {
		return nil, fmt.Errorf("failed to initialize database: %w", err)
	}

	if err := app.initListener(); err != nil {
		return nil, fmt.Errorf("failed to initialize listener: %w", err)
	}

	app.initBroker()
	app.initServer()

	return app, nil
}

// initDatabase initializes the database connection
func (app *App) initDatabase() error {
	log.Printf("Connecting to PostgreSQL with connection string: %s", app.config.DatabaseURL)

	db, err := sql.Open("postgres", app.config.DatabaseURL)
	if err != nil {
		return fmt.Errorf("failed to open database connection: %w", err)
	}

	log.Println("Testing database connection...")
	if err = db.Ping(); err != nil {
		db.Close()
		return fmt.Errorf("failed to ping database: %w", err)
	}

	log.Println("Successfully connected to PostgreSQL")
	app.db = db
	return nil
}

// initListener initializes the PostgreSQL notification listener
func (app *App) initListener() error {
	log.Println("Setting up PostgreSQL notification listener...")

	listener := pq.NewListener(
		app.config.DatabaseURL,
		app.config.ListenerMinReconnectInterval,
		app.config.ListenerMaxReconnectInterval,
		func(ev pq.ListenerEventType, err error) {
			if err != nil {
				log.Printf("Listener error: %v\n", err)
			}
			switch ev {
			case pq.ListenerEventConnected:
				log.Println("Listener connected to database")
			case pq.ListenerEventDisconnected:
				log.Println("Listener disconnected from database")
			case pq.ListenerEventReconnected:
				log.Println("Listener reconnected to database")
			}
		},
	)

	app.listener = listener
	return nil
}

// initBroker initializes the broker and starts background processes
func (app *App) initBroker() {
	app.broker = NewBroker(app.db, app.listener)

	// Start notification processor
	go func() {
		log.Println("Starting notification processor goroutine")
		for notification := range app.listener.Notify {
			log.Printf("Notification processor received: %+v", notification)
			app.broker.listenerCh <- notification
		}
		log.Println("Notification processor stopped")
	}()

	// Start broker notification listener
	go app.broker.ListenForNotifications()
}

// initServer initializes the HTTP server
func (app *App) initServer() {
	app.server = NewServer(app.broker, app.config)
}

// Run starts the application and handles graceful shutdown
func (app *App) Run() error {
	// Set up graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start server in goroutine
	serverErr := make(chan error, 1)
	go func() {
		if err := app.server.Start(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	// Wait for shutdown signal or server error
	select {
	case err := <-serverErr:
		return fmt.Errorf("server error: %w", err)
	case sig := <-sigChan:
		log.Printf("Received signal: %v", sig)
	}

	// Graceful shutdown
	return app.shutdown(ctx)
}

// shutdown gracefully shuts down the application
func (app *App) shutdown(ctx context.Context) error {
	shutdownCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	log.Println("Starting graceful shutdown...")

	// Shutdown HTTP server
	if err := app.server.Shutdown(shutdownCtx); err != nil {
		log.Printf("Error shutting down server: %v", err)
	}

	// Close listener
	if app.listener != nil {
		log.Println("Closing PostgreSQL listener...")
		app.listener.Close()
	}

	// Close database
	if app.db != nil {
		log.Println("Closing database connection...")
		app.db.Close()
	}

	log.Println("Graceful shutdown completed")
	return nil
}

func main() {
	log.Println("Starting application...")

	// Load configuration
	config := NewConfig()

	// Create and initialize application
	app, err := NewApp(config)
	if err != nil {
		log.Fatalf("Failed to create application: %v", err)
	}

	// Run application with graceful shutdown
	if err := app.Run(); err != nil {
		log.Fatalf("Application error: %v", err)
	}
}
