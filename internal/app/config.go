package app

import (
	"encoding/json"
	"fmt"

	"log/slog"

	"github.com/lestrrat-go/jwx/v4/jwa"

	"github.com/lestrrat-go/jwx/v4/jwk"
)

type Config struct {
	LogLevel      slog.Level `envconfig:"LOG_LEVEL"`
	Port          int        `envconfig:"PORT" default:"8080"`
	DeploymentURL string     `envconfig:"DEPLOYMENT_URL" required:"true"`
	// Json encoded array of signing keys [{id: "", pem: "", alg: "RSA256"}]
	// First key in array will be used for signing
	// All keys will be exposed on jwks endpoint
	RawSigningKeys []byte `envconfig:"SIGNING_KEYS" required:"true"`
}

type SigningKey struct {
	ID        string `json:"id"`
	PEM       string `json:"pem"`
	Algorithm string `json:"alg"`
}

func (c Config) SigningKeys() ([]jwk.Key, error) {
	var rawKeys []SigningKey
	err := json.Unmarshal(c.RawSigningKeys, &rawKeys)
	if err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}

	keys := make([]jwk.Key, 0, len(rawKeys))

	for _, rawKey := range rawKeys {
		key, err := jwk.ParseKey([]byte(rawKey.PEM), jwk.WithX509(true))
		if err != nil {
			return nil, fmt.Errorf("parse key: %w", err)
		}

		alg, ok := jwa.LookupKeyEncryptionAlgorithm(rawKey.Algorithm)
		if !ok {
			return nil, fmt.Errorf("invalid key alg: %s", rawKey.Algorithm)
		}

		key.Set(jwk.KeyIDKey, rawKey.ID)
		key.Set(jwk.KeyUsageKey, "sig")
		key.Set(jwk.AlgorithmKey, alg)

		keys = append(keys, key)
	}

	return keys, nil
}
