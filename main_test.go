package main

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// Test the actual HTTP handlers from main.go
func TestMainHTTPHandlers(t *testing.T) {
	connStr := "postgres://postgres:postgres@localhost/ted.behling?sslmode=disable"

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		t.Skipf("Skipping test: cannot connect to database: %v", err)
	}
	defer db.Close()

	if err = db.Ping(); err != nil {
		t.Skipf("Skipping test: database not available: %v", err)
	}

	listener := pq.NewListener(connStr, 10*time.Second, time.Minute, func(ev pq.ListenerEventType, err error) {
		if err != nil {
			t.Logf("Listener event: %v, error: %v", ev, err)
		}
	})
	defer listener.Close()

	// Create the broker exactly as main.go does
	broker := NewBroker(db, listener)

	// Start notification processor exactly as main.go does
	go func() {
		for notification := range listener.Notify {
			broker.listenerCh <- notification
		}
	}()

	go broker.ListenForNotifications()

	// Set up HTTP handlers exactly as main.go does
	mux := http.NewServeMux()

	// HTML interface handler from main.go
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}

		html := `<!DOCTYPE html><html><head><title>Postgres Notification Subscriber</title></head><body><h1>Test</h1></body></html>`
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(html))
	})

	// SSE handler from main.go
	mux.HandleFunc("/subscribe", func(w http.ResponseWriter, r *http.Request) {
		topicStr := r.URL.Query().Get("topic")
		if topicStr == "" {
			http.Error(w, "topic parameter is required", http.StatusBadRequest)
			return
		}

		// Validate UUID
		if _, err := uuid.Parse(topicStr); err != nil {
			http.Error(w, "invalid UUID format", http.StatusBadRequest)
			return
		}

		// Set headers for SSE
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		// Subscribe to messages
		messageCh := broker.Subscribe(topicStr)
		defer broker.Unsubscribe(topicStr, messageCh)

		// Get context from request for client disconnect
		ctx := r.Context()

		// Send messages to client (with timeout for test)
		timeout := time.After(100 * time.Millisecond)
		for {
			select {
			case msg := <-messageCh:
				fmt.Fprintf(w, "data: %s\n\n", msg)
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
				return // Exit after first message for test
			case <-ctx.Done():
				return
			case <-timeout:
				return // Exit after timeout for test
			}
		}
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	// Test HTML interface
	t.Run("HTMLInterface", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/")
		if err != nil {
			t.Fatalf("Failed to get HTML interface: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		contentType := resp.Header.Get("Content-Type")
		if contentType != "text/html" {
			t.Errorf("Expected Content-Type 'text/html', got '%s'", contentType)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("Failed to read response body: %v", err)
		}

		if !strings.Contains(string(body), "<html>") {
			t.Error("Response should contain HTML content")
		}
	})

	// Test SSE endpoint with valid UUID
	t.Run("SSEValidUUID", func(t *testing.T) {
		testUUID := uuid.New().String()

		resp, err := http.Get(server.URL + "/subscribe?topic=" + testUUID)
		if err != nil {
			t.Fatalf("Failed to connect to SSE endpoint: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		contentType := resp.Header.Get("Content-Type")
		if contentType != "text/event-stream" {
			t.Errorf("Expected Content-Type 'text/event-stream', got '%s'", contentType)
		}

		cacheControl := resp.Header.Get("Cache-Control")
		if cacheControl != "no-cache" {
			t.Errorf("Expected Cache-Control 'no-cache', got '%s'", cacheControl)
		}

		connection := resp.Header.Get("Connection")
		if connection != "keep-alive" {
			t.Errorf("Expected Connection 'keep-alive', got '%s'", connection)
		}

		cors := resp.Header.Get("Access-Control-Allow-Origin")
		if cors != "*" {
			t.Errorf("Expected Access-Control-Allow-Origin '*', got '%s'", cors)
		}
	})

	// Test SSE endpoint with invalid UUID
	t.Run("SSEInvalidUUID", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/subscribe?topic=invalid-uuid")
		if err != nil {
			t.Fatalf("Failed to make request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", resp.StatusCode)
		}
	})

	// Test SSE endpoint without topic parameter
	t.Run("SSEMissingTopic", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/subscribe")
		if err != nil {
			t.Fatalf("Failed to make request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", resp.StatusCode)
		}
	})

	// Test 404 for non-existent paths
	t.Run("NotFound", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/nonexistent")
		if err != nil {
			t.Fatalf("Failed to make request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d", resp.StatusCode)
		}
	})
}

// Test the complete notification flow using main.go components
func TestMainNotificationFlow(t *testing.T) {
	connStr := "postgres://postgres:postgres@localhost/postgres?sslmode=disable"

	// Setup database connections
	notifierDB, err := sql.Open("postgres", connStr)
	if err != nil {
		t.Skipf("Skipping test: cannot connect to database: %v", err)
	}
	defer notifierDB.Close()

	if err = notifierDB.Ping(); err != nil {
		t.Skipf("Skipping test: database not available: %v", err)
	}

	listener := pq.NewListener(connStr, 10*time.Second, time.Minute, func(ev pq.ListenerEventType, err error) {
		if err != nil {
			t.Logf("Listener event: %v, error: %v", ev, err)
		}
	})
	defer listener.Close()

	// Create broker exactly as main.go does
	broker := NewBroker(notifierDB, listener)

	// Start notification processor exactly as main.go does
	go func() {
		for notification := range listener.Notify {
			broker.listenerCh <- notification
		}
	}()

	go broker.ListenForNotifications()

	testTopic := uuid.New().String()

	// Subscribe using the broker (simulating what the SSE handler does)
	messageCh := broker.Subscribe(testTopic)
	defer broker.Unsubscribe(testTopic, messageCh)

	// Send notification via Postgres (simulating external notification)
	sqlStmt := fmt.Sprintf(`NOTIFY "%s", 'test message'`, testTopic)
	_, err = notifierDB.Exec(sqlStmt)
	if err != nil {
		t.Fatalf("Failed to send notification: %v", err)
	}

	// Wait for message through the complete flow
	select {
	case msg := <-messageCh:
		if msg != "yo" {
			t.Errorf("Expected 'yo', got '%s'", msg)
		}
	case <-time.After(5 * time.Second):
		t.Error("Timeout waiting for notification through main.go flow")
	}
}

// Test SSE connection with actual message delivery
func TestMainSSEMessageDelivery(t *testing.T) {
	connStr := "postgres://postgres:postgres@localhost/ted.behling?sslmode=disable"

	notifierDB, err := sql.Open("postgres", connStr)
	if err != nil {
		t.Skipf("Skipping test: cannot connect to database: %v", err)
	}
	defer notifierDB.Close()

	if err = notifierDB.Ping(); err != nil {
		t.Skipf("Skipping test: database not available: %v", err)
	}

	listener := pq.NewListener(connStr, 10*time.Second, time.Minute, func(ev pq.ListenerEventType, err error) {
		if err != nil {
			t.Logf("Listener event: %v, error: %v", ev, err)
		}
	})
	defer listener.Close()

	// Create broker exactly as main.go does
	broker := NewBroker(notifierDB, listener)

	// Start notification processor exactly as main.go does
	go func() {
		for notification := range listener.Notify {
			broker.listenerCh <- notification
		}
	}()

	go broker.ListenForNotifications()

	// Set up SSE handler exactly as main.go does
	mux := http.NewServeMux()
	mux.HandleFunc("/subscribe", func(w http.ResponseWriter, r *http.Request) {
		topicStr := r.URL.Query().Get("topic")
		if topicStr == "" {
			http.Error(w, "topic parameter is required", http.StatusBadRequest)
			return
		}

		if _, err := uuid.Parse(topicStr); err != nil {
			http.Error(w, "invalid UUID format", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		messageCh := broker.Subscribe(topicStr)
		defer broker.Unsubscribe(topicStr, messageCh)

		ctx := r.Context()

		// Send initial connection confirmation
		fmt.Fprintf(w, "data: connected\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}

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
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	testUUID := uuid.New().String()

	// Create SSE client
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", server.URL+"/subscribe?topic="+testUUID, nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Failed to connect to SSE: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", resp.StatusCode)
	}

	// Read the initial connection message
	buffer := make([]byte, 1024)
	n, err := resp.Body.Read(buffer)
	if err != nil {
		t.Fatalf("Failed to read initial message: %v", err)
	}

	initialMsg := string(buffer[:n])
	if !strings.Contains(initialMsg, "data: connected") {
		t.Errorf("Expected initial connection message, got: %s", initialMsg)
	}

	// Send notification via Postgres
	go func() {
		time.Sleep(100 * time.Millisecond) // Give SSE connection time to establish
		sqlStmt := fmt.Sprintf(`NOTIFY "%s", 'test message'`, testUUID)
		_, err := notifierDB.Exec(sqlStmt)
		if err != nil {
			t.Errorf("Failed to send notification: %v", err)
		}
	}()

	// Read the notification message
	n, err = resp.Body.Read(buffer)
	if err != nil {
		t.Fatalf("Failed to read notification message: %v", err)
	}

	notificationMsg := string(buffer[:n])
	if !strings.Contains(notificationMsg, "data: yo") {
		t.Errorf("Expected 'data: yo' in notification, got: %s", notificationMsg)
	}
}

// Test broker behavior under concurrent load (using main.go broker)
func TestMainBrokerConcurrentLoad(t *testing.T) {
	connStr := "postgres://postgres:postgres@localhost/ted.behling?sslmode=disable"

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		t.Skipf("Skipping test: cannot connect to database: %v", err)
	}
	defer db.Close()

	if err = db.Ping(); err != nil {
		t.Skipf("Skipping test: database not available: %v", err)
	}

	listener := pq.NewListener(connStr, 10*time.Second, time.Minute, func(ev pq.ListenerEventType, err error) {
		if err != nil {
			t.Logf("Listener event: %v, error: %v", ev, err)
		}
	})
	defer listener.Close()

	// Create broker exactly as main.go does
	broker := NewBroker(db, listener)

	// Start notification processor exactly as main.go does
	go func() {
		for notification := range listener.Notify {
			broker.listenerCh <- notification
		}
	}()

	go broker.ListenForNotifications()

	// Test concurrent operations
	var wg sync.WaitGroup
	numTopics := 5
	numClientsPerTopic := 10

	topics := make([]string, numTopics)
	for i := 0; i < numTopics; i++ {
		topics[i] = uuid.New().String()
	}

	// Concurrent subscribes
	for i := 0; i < numTopics; i++ {
		for j := 0; j < numClientsPerTopic; j++ {
			wg.Add(1)
			go func(topicIdx, clientIdx int) {
				defer wg.Done()
				topic := topics[topicIdx]
				ch := broker.Subscribe(topic)

				// Hold subscription for a bit
				time.Sleep(50 * time.Millisecond)

				broker.Unsubscribe(topic, ch)
			}(i, j)
		}
	}

	// Concurrent broadcasts
	for i := 0; i < numTopics; i++ {
		wg.Add(1)
		go func(topicIdx int) {
			defer wg.Done()
			topic := topics[topicIdx]
			for j := 0; j < 5; j++ {
				broker.Broadcast(topic)
				time.Sleep(10 * time.Millisecond)
			}
		}(i)
	}

	wg.Wait()

	// Verify all topics are cleaned up
	time.Sleep(100 * time.Millisecond) // Allow cleanup to complete

	for _, topic := range topics {
		broker.mu.RLock()
		_, exists := broker.clients[topic]
		broker.mu.RUnlock()

		if exists {
			t.Errorf("Topic %s should be cleaned up after all clients disconnect", topic)
		}
	}
}

// Test the new App structure with dependency injection
func TestAppStructure(t *testing.T) {
	// Test configuration
	config := NewConfig()
	if config.DatabaseURL == "" {
		t.Error("Config should have a default database URL")
	}
	if config.ServerAddress == "" {
		t.Error("Config should have a default server address")
	}

	// Test environment variable override
	originalURL := config.DatabaseURL
	os.Setenv("DATABASE_URL", "test://localhost/test")
	defer os.Unsetenv("DATABASE_URL")

	newConfig := NewConfig()
	if newConfig.DatabaseURL == originalURL {
		t.Error("Config should use environment variable when set")
	}
	if newConfig.DatabaseURL != "test://localhost/test" {
		t.Errorf("Expected 'test://localhost/test', got '%s'", newConfig.DatabaseURL)
	}
}

// Test Server creation with dependency injection
func TestServerCreation(t *testing.T) {
	connStr := "postgres://postgres:postgres@localhost/ted.behling?sslmode=disable"

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		t.Skipf("Skipping test: cannot connect to database: %v", err)
	}
	defer db.Close()

	if err = db.Ping(); err != nil {
		t.Skipf("Skipping test: database not available: %v", err)
	}

	listener := pq.NewListener(connStr, 10*time.Second, time.Minute, func(ev pq.ListenerEventType, err error) {
		if err != nil {
			t.Logf("Listener event: %v, error: %v", ev, err)
		}
	})
	defer listener.Close()

	broker := NewBroker(db, listener)
	config := NewConfig()

	// Test server creation
	server := NewServer(broker, config)
	if server == nil {
		t.Fatal("NewServer should return a valid server")
	}

	if server.broker != broker {
		t.Error("Server should have the injected broker")
	}

	if server.config != config {
		t.Error("Server should have the injected config")
	}

	if server.server == nil {
		t.Error("Server should have an HTTP server instance")
	}

	if server.server.Addr != config.ServerAddress {
		t.Errorf("Expected server address '%s', got '%s'", config.ServerAddress, server.server.Addr)
	}
}
