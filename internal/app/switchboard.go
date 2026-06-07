package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
)

var ErrConnectionNotFound = errors.New("connection not found")

type Signal struct {
	Message []byte
	Result  chan<- error
}

type SwitchBoard struct {
	mu          sync.RWMutex
	connections map[string]chan<- Signal
}

func (sb *SwitchBoard) Register(id string, conn chan<- Signal) {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	if _, ok := sb.connections[id]; ok {
		slog.Warn("Registering over already existing connection")
	}

	sb.connections[id] = conn
}

func (sb *SwitchBoard) Unregister(id string) {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	if _, ok := sb.connections[id]; !ok {
		slog.Warn("Unregistering missing connection")
	}

	delete(sb.connections, id)
}

func (sb *SwitchBoard) getConnection(id string) (chan<- Signal, error) {
	sb.mu.RLock()
	defer sb.mu.RUnlock()
	conn, ok := sb.connections[id]

	if !ok {
		return nil, ErrConnectionNotFound
	}
	return conn, nil
}

func (sb *SwitchBoard) SendMessage(ctx context.Context, id string, message []byte) error {
	conn, err := sb.getConnection(id)
	if err != nil {
		return fmt.Errorf("get connection: %w", err)
	}

	result := make(chan error)
	defer close(result)

	signal := Signal{
		Result:  result,
		Message: message,
	}

	select {
	case conn <- signal:
	case <-ctx.Done():
		return fmt.Errorf("send message: %w", ctx.Err())
	}

	select {
	case err := <-result:
		if err != nil {
			return fmt.Errorf("received result: %w", err)
		}

		return nil

	case <-ctx.Done():
		return fmt.Errorf("waiting for message ack: %w", ctx.Err())
	}
}
