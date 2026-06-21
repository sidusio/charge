package app

import (
	"encoding/json"
	"fmt"
	"time"

	"log/slog"
)

type Config struct {
	LogLevel      slog.Level `envconfig:"LOG_LEVEL"`
	Port          int        `envconfig:"PORT" default:"8080"`
	DeploymentURL string     `envconfig:"DEPLOYMENT_URL" required:"true"`
	// Json encoded array of signing keys [{id: "", pem: "", alg: "RSA256"}]
	// First key in array will be used for signing
	// All keys will be exposed on jwks endpoint
	RawSigningKeys        []byte        `envconfig:"SIGNING_KEYS" required:"true"`
	MaxConnectionDuration time.Duration `envconfig:"MAX_CONNECTION_DURATION" default:"4h"`
	// An empty value/list allows all origins
	AllowedOrigins       []string `envconfig:"ALLOWED_ORIGINS"`
	AllowInsecureOrigins bool     `envconfig:"ALLOW_INSECURE_ORIGINS"`
}

type SigningKey struct {
	ID        string `json:"id"`
	PEM       string `json:"pem"`
	Algorithm string `json:"alg"`
}

func (c Config) SigningKeys() ([]SigningKey, error) {
	var keys []SigningKey
	err := json.Unmarshal(c.RawSigningKeys, &keys)
	if err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	return keys, nil
}
