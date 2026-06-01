package app

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"sync"
	"time"

	"github.com/jwx-go/jwkfetch/v4"
	"github.com/lestrrat-go/jwx/v4/jwt"
	"golang.org/x/sync/errgroup"
	"sidus.io/notman/internal/util"
)

type BackendIndex struct {
	mu      sync.RWMutex
	backens map[string]*Backend
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

	be, err := NewBackend(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("new backend: %w", err)
	}

	return i.getOrSetBackend(issuer, be), nil

}

type Backend struct {
	board     *SwitchBoard
	eventsURL url.URL
}

func NewBackend(ctx context.Context, issuer string) (*Backend, error) {
	cache, err := jwkfetch.NewCache(ctx, jwkfetch.NewClient())

	return nil, nil
}

func (b *Backend) KeySet() {

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

func Run(ctx context.Context, log *slog.Logger, cfg Config) error {
	eg, ctx := errgroup.WithContext(ctx)

	mux := http.NewServeMux()

	bi := BackendIndex{
		backens: make(map[string]*Backend),
	}

	mux.HandleFunc("/sse", func(w http.ResponseWriter, r *http.Request) {
		ctx = r.Context()

		rawToken := r.URL.Query().Get("token")
		if rawToken == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		unsafeToken, err := jwt.ParseInsecure([]byte(rawToken))
		if err != nil {
			log.Warn("Failed to parse token", "error", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		audience, _ := unsafeToken.Audience()
		if !slices.Contains(audience, cfg.DeploymentIdentifier) {
			log.Warn("Invalid audience", "audience", audience)
			w.WriteHeader(http.StatusForbidden)
			return
		}

		issuer, _ := unsafeToken.Issuer()

		be, err := bi.GetBackend(ctx, issuer)
		if err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}

		token, err := jwt.Parse([]byte(rawToken), jwt.WithVerifyAuto(be.KeySet()))

		// Read ?token
		// Figure out who is backend

		id := "abc"

		// Create channel
		signals := make(chan Signal)
		defer close(signals)

		be.Connect(ctx, id, signals)
		defer be.Disconnect(ctx, id)

		// Loop over channel and write
		for {
			select {
			case s, ok := <-signals:
				if !ok {
					return
				}
				_, err := w.Write(s.Message)
				if err != nil {
					select {
					case s.Result <- fmt.Errorf("write: %w", err):
					case <-ctx.Done():
						return
					}
				}
				close(s.Result)
			case <-ctx.Done():
				return
			}
		}
	})

	server := &http.Server{
		Handler: http.MaxBytesHandler(mux, 1<<20 /* 1mb */),
		Addr:    fmt.Sprintf(":%d", cfg.Port),
	}

	eg.Go(func() error {
		err := util.ListenAndServe(ctx, server, time.Second*10)
		return util.Wrap("http server failed", err)
	})

	return eg.Wait()
}
