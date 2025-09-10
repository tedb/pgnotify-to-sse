package main

import (
	"database/sql"
	"log"
	"strings"
	"sync"

	"github.com/lib/pq"
)

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
