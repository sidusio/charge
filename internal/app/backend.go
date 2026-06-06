package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/singleflight"
)

type Bouncer struct {
	sfGroup SingleFlightGroup[*BounceStatus]
	m       SyncMap[string, *BounceStatus]
}

type BounceStatus struct {
	mu          sync.RWMutex
	validBefore time.Time
	allowed     bool
}

var ErrNotAllowed = errors.New("not allowed")

func (b *Bouncer) Allowed(domain string) error {
	status, ok := b.m.Load(domain)
	if !ok || time.Now().After(status.validBefore) {
		return b.checkWebserver(domain)
	}

	if !status.allowed {
		return ErrNotAllowed
	}

	return nil
}

type SingleFlightGroup[T any] struct {
	internal *singleflight.Group
}

func (g SingleFlightGroup[T]) Do(key string, fn func() (T, error)) (T, error, bool) {
	res, err, shared := g.internal.Do(key, func() (any, error) {
		res, err := fn()
		return res, err
	})
	return res.(T), err, shared
}

func (b *Bouncer) checkWebserver(domain string) error {
	b.sfGroup.Do(domain, func() (*BounceStatus, error) {
		panic("not done yet")
	})

	panic("not done yet")
}

type BackendIndex struct {
	mu      sync.RWMutex
	backens map[string]*Backend
	sign    SignFunc
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

func (i *BackendIndex) GetBackend(ctx context.Context, callbackURL string) (*Backend, error) {
	be := i.getBackend(callbackURL)
	if be != nil {
		return be, nil
	}

	be, err := NewBackend(ctx, callbackURL, i.sign)
	if err != nil {
		return nil, fmt.Errorf("new backend: %w", err)
	}

	return i.getOrSetBackend(callbackURL, be), nil
}

type Backend struct {
	board     *SwitchBoard
	issuer    string
	eventsURL url.URL
	sign      SignFunc
}

type SignFunc func(body []byte) ([]byte, error)

func NewBackend(ctx context.Context, callbackURL string, sign SignFunc) (*Backend, error) {

	return &Backend{
		board: &SwitchBoard{
			connections: make(map[string]chan<- Signal),
		},
		sign: sign,
	}, nil
}

type CloudEvent[Data any] struct {
	Specversion string
	Id          string
	Source      string
	Time        time.Time
	Type        string
	Data        Data
}

type ConnectBody struct {
	ClientToken string `json:"clientToken"`
	SendToken   string `json:"sendToken"`
}

func (b *Backend) Connect(ctx context.Context, token string, signals chan<- Signal) error {
	connectionId := uuid.New().String()
	event := CloudEvent[ConnectBody]{
		Specversion: "1.0",
		Id:          uuid.New().String(),
		Source:      "", // TODO: deployment url
		Time:        time.Now(),
		Type:        "notman.connected.v1",
		Data: ConnectBody{
			ClientToken: token,
			SendToken:   "", // TODO:
		},
	}

	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	signature, err := b.sign(body)
	if err != nil {
		return fmt.Errorf("sign: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, b.eventsURL.String(), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	req.Header.Add("Content-Type", "application/cloudevents+json")
	req.Header.Add("Webhook-Signature", string(signature))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("post connected: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return errors.New("non-200 status code received")
	}

	b.board.Register(connectionId, signals)

	return nil
}

func (b *Backend) Disconnect(ctx context.Context, token string) error {
	b.board.Unregister(token)

	_, err := http.Post(b.eventsURL.String(), "text/plain", bytes.NewBufferString(token))
	if err != nil {
		return fmt.Errorf("post disconnected: %w", err)
	}

	return nil
}
