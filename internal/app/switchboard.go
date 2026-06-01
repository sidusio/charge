package app

import (
	"errors"
	"sync"
)

var ErrConnectionNotFound = errors.New("connection not found")

type SwitchBoard struct {
	mu          sync.RWMutex
	connections map[string]chan<- []byte
}

func (sb *SwitchBoard) Register(id string, conn chan<- []byte) {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	existing, ok := sb.connections[id]
	if ok {
		close(existing)
	}
	sb.connections[id] = conn
}

func (sb *SwitchBoard) Unregister(id string) {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	conn, ok := sb.connections[id]
	if ok {
		close(conn)
	}

	delete(sb.connections, id)
}

func (sb *SwitchBoard) SendMessage(id string, message []byte) error {
	sb.mu.RLock()
	conn, ok := sb.connections[id]
	sb.mu.RUnlock()

	if !ok {
		return ErrConnectionNotFound
	}

	conn <- message

	return nil
}
