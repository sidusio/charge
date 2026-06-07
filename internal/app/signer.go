package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/lestrrat-go/jwx/v4/jwa"
	"github.com/lestrrat-go/jwx/v4/jwk"
	"github.com/lestrrat-go/jwx/v4/jws"
	"github.com/lestrrat-go/jwx/v4/jwt"
)

const (
	purpoeClaimKey = "purpose"
	purposeSend    = "send"
	callbackURLKey = "callback_url"
)

type SendTokenData struct {
	ConnectionId string
	CallbackURL  string
}

type Signer struct {
	alg           jwa.KeyAlgorithm
	key           jwk.Key
	jwksPayload   []byte
	publicJWKS    jwk.Set
	deploymentURL string
}

func NewSigner(rawKeys []SigningKey, deploymentURL string) (Signer, error) {
	privateKeys := make([]jwk.Key, 0, len(rawKeys))

	for i, rawKey := range rawKeys {
		key, err := jwk.ParseKey([]byte(rawKey.PEM), jwk.WithX509(true))
		if err != nil {
			return Signer{}, fmt.Errorf("parse key [%d]: %w", i, err)
		}

		algs, err := jws.AlgorithmsForKey(key)
		if err != nil {
			return Signer{}, fmt.Errorf("get algorithms from key [%d]: %w", i, err)
		}

		key.Set(jwk.KeyIDKey, rawKey.ID)
		key.Set(jwk.KeyUsageKey, "sig")
		key.Set(jwk.AlgorithmKey, algs[0])

		privateKeys = append(privateKeys, key)
	}

	if len(privateKeys) < 1 {
		return Signer{}, errors.New("no signing keys added")
	}

	signingKey := privateKeys[0]
	signingAlg, ok := signingKey.Algorithm()
	if !ok {
		return Signer{}, errors.New("no signing key algoritm")
	}

	publicJWKS := jwk.NewSet()
	for i, privateKey := range privateKeys {
		publicKey, err := privateKey.PublicKey()
		if err != nil {
			return Signer{}, fmt.Errorf("extract public key [%d]: %s", i, err)
		}
		publicJWKS.AddKey(publicKey)
	}

	if publicJWKS.Len() < 1 {
		return Signer{}, errors.New("no public keys added")
	}

	jwksPayload, err := json.Marshal(publicJWKS)
	if err != nil {
		return Signer{}, fmt.Errorf("creating jwks payload: %w", err)
	}

	return Signer{
		alg:           signingAlg,
		key:           signingKey,
		jwksPayload:   jwksPayload,
		publicJWKS:    publicJWKS,
		deploymentURL: deploymentURL,
	}, nil
}

func (s Signer) JWKsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write(s.jwksPayload)
}

func (s Signer) SignDetatched(body []byte) ([]byte, error) {
	signature, err := jws.Sign(body, jws.WithKey(s.alg, s.key))
	if err != nil {
		return nil, fmt.Errorf("sign body: %w", err)
	}

	parts := strings.Split(string(signature), ".")
	if len(parts) != 3 {
		return nil, errors.New("signature not in 3 parts")
	}

	detachedJWS := fmt.Sprintf("%s..%s", parts[0], parts[2])
	return []byte(detachedJWS), nil
}

func (s Signer) CreateSendToken(data SendTokenData, expireAt time.Time) ([]byte, error) {
	token, err := jwt.NewBuilder().
		Issuer(s.deploymentURL).
		Audience([]string{s.deploymentURL}).
		Subject(data.ConnectionId).
		Expiration(expireAt).
		NotBefore(time.Now()).
		IssuedAt(time.Now()).
		Claim(purpoeClaimKey, purposeSend).
		Claim(callbackURLKey, data.CallbackURL).
		Build()
	if err != nil {
		return nil, fmt.Errorf("build token: %w", err)
	}

	signed, err := jwt.Sign(token, jwt.WithKey(s.alg, s.key))
	if err != nil {
		return nil, fmt.Errorf("sign token: %w", err)
	}

	return signed, nil
}

func (s Signer) ParseAndValidateSendToken(token []byte) (SendTokenData, error) {
	parsed, err := jwt.Parse(
		token,
		jwt.WithKeySet(s.publicJWKS),
		jwt.WithValidate(true),
		jwt.WithAudience(s.deploymentURL),
		jwt.WithIssuer(s.deploymentURL),
		jwt.WithRequiredClaim(callbackURLKey),
		jwt.WithClaimValue(purpoeClaimKey, purposeSend),
	)
	if err != nil {
		return SendTokenData{}, fmt.Errorf("parse token: %w", err)
	}

	connectionId, ok := parsed.Subject()
	if !ok {
		return SendTokenData{}, errors.New("token missing subject")
	}

	callbackURL, ok := parsed.Field(callbackURLKey)
	if !ok {
		return SendTokenData{}, errors.New("token missing callback_url claim")
	}

	callbackURLStr, ok := callbackURL.(string)
	if !ok {
		return SendTokenData{}, errors.New("callback_url claim is not a string")
	}

	return SendTokenData{
		ConnectionId: connectionId,
		CallbackURL:  callbackURLStr,
	}, nil
}
