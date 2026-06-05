package app

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sync"

	"github.com/jwx-go/jwkfetch/v4"
)

type BackendIndex struct {
	mu       sync.RWMutex
	backens  map[string]*Backend
	jwkCache *jwkfetch.Cache
}

func (i *BackendIndex) getBackend(issuer string) *Backend {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.backens[issuer]
}

func (i *BackendIndex) getOrSetBackend(issuer string, new *Backend) *Backend {
	i.mu.Lock()
	defer i.mu.Unlock()
	be, ok := i.backens[issuer]
	if ok {
		return be
	}

	i.backens[issuer] = new
	return new
}

func (i *BackendIndex) GetBackend(ctx context.Context, issuer string) (*Backend, error) {
	be := i.getBackend(issuer)
	if be != nil {
		return be, nil
	}

	be, err := NewBackend(ctx, issuer, i.jwkCache)
	if err != nil {
		return nil, fmt.Errorf("new backend: %w", err)
	}

	return i.getOrSetBackend(issuer, be), nil

}

type Backend struct {
	board     *SwitchBoard
	issuer    string
	eventsURL url.URL
	jwkCache  *jwkfetch.Cache
}

func NewBackend(ctx context.Context, issuer string, cache *jwkfetch.Cache) (*Backend, error) {
	return &Backend{
		board: &SwitchBoard{
			connections: make(map[string]chan<- Signal),
		},
		jwkCache: cache,
	}, nil
}

func (b *Backend) KeySet() *jwkfetch.Cache {
	return b.jwkCache
}

func (b *Backend) Connect(ctx context.Context, id string, signals chan<- Signal) error {
	b.board.Register(id, signals)

	_, err := http.Post(b.eventsURL.String(), "text/plain", bytes.NewBufferString("connected"))
	if err != nil {
		return fmt.Errorf("post connected: %w", err)
	}

	return nil
}

func (b *Backend) Disconnect(ctx context.Context, id string) error {
	b.board.Unregister(id)

	_, err := http.Post(b.eventsURL.String(), "text/plain", bytes.NewBufferString("disconnected"))
	if err != nil {
		return fmt.Errorf("post disconnected: %w", err)
	}

	return nil
}
