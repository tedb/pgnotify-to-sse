package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lib/pq"
)

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
