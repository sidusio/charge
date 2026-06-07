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
)

type BackendIndex struct {
	mu            sync.RWMutex
	backens       map[string]*Backend
	signer        Signer
	deploymentURL string
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

	be, err := NewBackend(ctx, callbackURL, i.signer, i.deploymentURL)
	if err != nil {
		return nil, fmt.Errorf("new backend: %w", err)
	}

	return i.getOrSetBackend(callbackURL, be), nil
}

type Backend struct {
	board         *SwitchBoard
	callbackUrl   url.URL
	signer        Signer
	deploymentURL string
}

func NewBackend(ctx context.Context, rawCallbackURL string, signer Signer, deploymentURL string) (*Backend, error) {
	callbackURL, err := url.Parse(rawCallbackURL)
	if err != nil {
		return nil, fmt.Errorf("parse callbackURL %s: %w", rawCallbackURL, err)
	}

	return &Backend{
		board: &SwitchBoard{
			connections: make(map[string]chan<- Signal),
		},
		signer:        signer,
		deploymentURL: deploymentURL,
		callbackUrl:   *callbackURL,
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
	ClientToken  string `json:"clientToken"`
	SendToken    string `json:"sendToken"`
	ConnectionId string `json:"connectionId"`
}

func (b *Backend) Connect(ctx context.Context, token string, signals chan<- Signal, maxLifeTime time.Duration) (string, error) {
	connectionId := uuid.New().String()
	sendToken, err := b.signer.CreateSendToken(SendTokenData{
		ConnectionId: connectionId,
		CallbackURL:  b.callbackUrl.String(),
	}, time.Now().Add(maxLifeTime))
	if err != nil {
		return "", fmt.Errorf("create send token: %w", err)
	}

	event := CloudEvent[ConnectBody]{
		Specversion: "1.0",
		Id:          uuid.New().String(),
		Source:      b.deploymentURL,
		Time:        time.Now(),
		Type:        "charge.connected.v1",
		Data: ConnectBody{
			ClientToken:  token,
			SendToken:    string(sendToken),
			ConnectionId: connectionId,
		},
	}

	body, err := json.Marshal(event)
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}

	signature, err := b.signer.SignDetatched(body)
	if err != nil {
		return "", fmt.Errorf("sign: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, b.callbackUrl.String(), bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("new request: %w", err)
	}
	req.Header.Add("Content-Type", "application/cloudevents+json")
	req.Header.Add("Webhook-Signature", string(signature))

	// We register the connection before sending the request to
	// ensure that if the callback immediately tries to send a message,
	// the connection will be ready to receive it.
	b.board.Register(connectionId, signals)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		b.board.Unregister(connectionId)
		return "", fmt.Errorf("post connected: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		b.board.Unregister(connectionId)
		return "", errors.New("non-200 status code received")
	}

	return connectionId, nil
}

func (b *Backend) Disconnect(ctx context.Context, connectionId string) error {
	b.board.Unregister(connectionId)

	event := CloudEvent[ConnectBody]{
		Specversion: "1.0",
		Id:          uuid.New().String(),
		Source:      b.deploymentURL,
		Time:        time.Now(),
		Type:        "charge.disconnected.v1",
		Data: ConnectBody{
			ConnectionId: connectionId,
		},
	}

	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	signature, err := b.signer.SignDetatched(body)
	if err != nil {
		return fmt.Errorf("sign: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, b.callbackUrl.String(), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	req.Header.Add("Content-Type", "application/cloudevents+json")
	req.Header.Add("Webhook-Signature", string(signature))

	_, err = http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("post disconnected: %w", err)
	}

	return nil
}

func (b *Backend) SendMessage(ctx context.Context, connectionId string, message []byte) error {
	return b.board.SendMessage(ctx, connectionId, message)
}
