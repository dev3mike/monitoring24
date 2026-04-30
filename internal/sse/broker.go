package sse

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Event is a typed SSE message.
type Event struct {
	Type string
	Data json.RawMessage
}

// client represents a connected SSE subscriber.
type client struct {
	id   string
	send chan Event
}

// Broker manages SSE client connections and broadcasts events.
type Broker struct {
	mu         sync.RWMutex
	clients    map[string]*client
	broadcast  chan Event
	register   chan *client
	unregister chan *client
}

func NewBroker() *Broker {
	return &Broker{
		clients:    make(map[string]*client),
		broadcast:  make(chan Event, 256),
		register:   make(chan *client, 16),
		unregister: make(chan *client, 16),
	}
}

// Run processes registrations, unregistrations, and broadcasts until ctx is done.
func (b *Broker) Run(done <-chan struct{}) {
	heartbeat := time.NewTicker(30 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case c := <-b.register:
			b.mu.Lock()
			b.clients[c.id] = c
			b.mu.Unlock()

		case c := <-b.unregister:
			b.mu.Lock()
			if _, ok := b.clients[c.id]; ok {
				delete(b.clients, c.id)
				close(c.send)
			}
			b.mu.Unlock()

		case evt := <-b.broadcast:
			b.mu.RLock()
			for _, c := range b.clients {
				select {
				case c.send <- evt:
				default:
					// slow client — drop event; it will resync via REST on reconnect
				}
			}
			b.mu.RUnlock()

		case <-heartbeat.C:
			b.Broadcast("heartbeat", json.RawMessage(`{}`))

		case <-done:
			return
		}
	}
}

// Broadcast enqueues a typed event for all connected clients.
func (b *Broker) Broadcast(eventType string, data json.RawMessage) {
	select {
	case b.broadcast <- Event{Type: eventType, Data: data}:
	default:
	}
}

// BroadcastJSON marshals v and broadcasts it under eventType.
func (b *Broker) BroadcastJSON(eventType string, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	b.Broadcast(eventType, data)
}

// ClientCount returns the number of currently connected SSE clients.
func (b *Broker) ClientCount() int {
	b.mu.RLock()
	n := len(b.clients)
	b.mu.RUnlock()
	return n
}

// ServeHTTP handles an individual SSE connection.
func (b *Broker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable nginx buffering

	c := &client{
		id:   newID(),
		send: make(chan Event, 64),
	}
	b.register <- c
	defer func() { b.unregister <- c }()

	for {
		select {
		case evt, ok := <-c.send:
			if !ok {
				return
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", evt.Type, evt.Data)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func newID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}
