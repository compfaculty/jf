package server

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"jf/internal/models"
)

// EventType represents the type of SSE event
type EventType string

const (
	EventJobFound     EventType = "job_found"
	EventScanStatus   EventType = "scan_status"
	EventScanComplete EventType = "scan_complete"
)

// Event represents an SSE event
type Event struct {
	Type EventType   `json:"type"`
	Data interface{} `json:"data"`
}

// Client represents an SSE client connection
type Client struct {
	ID       string
	Messages chan []byte
	Done     chan struct{}
}

// Broker manages all SSE client connections
type Broker struct {
	mu      sync.RWMutex
	clients map[string]*Client
	closed  bool
}

// NewBroker creates a new SSE event broker
func NewBroker() *Broker {
	return &Broker{
		clients: make(map[string]*Client),
	}
}

// Register adds a new client to the broker
func (b *Broker) Register(id string) *Client {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return nil
	}

	client := &Client{
		ID:       id,
		Messages: make(chan []byte, 10), // Buffered to avoid blocking
		Done:     make(chan struct{}),
	}
	b.clients[id] = client
	log.Printf("[SSE] client registered: %s (total: %d)", id, len(b.clients))
	return client
}

// Unregister removes a client from the broker
func (b *Broker) Unregister(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if client, exists := b.clients[id]; exists {
		close(client.Messages)
		close(client.Done)
		delete(b.clients, id)
		log.Printf("[SSE] client unregistered: %s (total: %d)", id, len(b.clients))
	}
}

// Broadcast sends an event to all registered clients (non-blocking)
func (b *Broker) Broadcast(evt Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.closed || len(b.clients) == 0 {
		return
	}

	data, err := json.Marshal(evt.Data)
	if err != nil {
		log.Printf("[SSE] error marshaling event: %v", err)
		return
	}

	// Format SSE message
	msg := formatSSEMessage(string(evt.Type), string(data))

	// Send to all clients (non-blocking - skip if channel is full)
	for id, client := range b.clients {
		select {
		case client.Messages <- msg:
			// Successfully sent
		case <-client.Done:
			// Client disconnected, will be cleaned up
		default:
			// Channel full, skip this client (non-blocking)
			log.Printf("[SSE] client %s message channel full, skipping", id)
		}
	}
}

// SendJobFound broadcasts a job_found event
func (b *Broker) SendJobFound(job models.Job) {
	b.Broadcast(Event{
		Type: EventJobFound,
		Data: job,
	})
}

// SendScanStatus broadcasts a scan_status event
func (b *Broker) SendScanStatus(running bool, percent, found, total int, errMsg string) {
	b.Broadcast(Event{
		Type: EventScanStatus,
		Data: map[string]interface{}{
			"running":   running,
			"percent":   percent,
			"found":     found,
			"total":     total,
			"error":     errMsg,
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		},
	})
}

// SendScanComplete broadcasts a scan_complete event
func (b *Broker) SendScanComplete(totalFound int, duration time.Duration) {
	b.Broadcast(Event{
		Type: EventScanComplete,
		Data: map[string]interface{}{
			"total_found":      totalFound,
			"duration_seconds": duration.Seconds(),
			"timestamp":        time.Now().UTC().Format(time.RFC3339),
		},
	})
}

// ClientCount returns the number of active clients
func (b *Broker) ClientCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.clients)
}

// Close shuts down the broker and closes all client connections
func (b *Broker) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return
	}

	b.closed = true
	for id, client := range b.clients {
		close(client.Messages)
		close(client.Done)
		delete(b.clients, id)
	}
	log.Printf("[SSE] broker closed, all clients disconnected")
}

// formatSSEMessage formats a message according to SSE protocol
func formatSSEMessage(event, data string) []byte {
	// SSE format: "event: <type>\ndata: <json>\n\n"
	return []byte("event: " + event + "\ndata: " + data + "\n\n")
}
