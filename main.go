package main

import (
	"log"
)

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
