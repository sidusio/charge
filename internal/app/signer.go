package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/lestrrat-go/jwx/v4/jwa"
	"github.com/lestrrat-go/jwx/v4/jwk"
	"github.com/lestrrat-go/jwx/v4/jws"
)

type Signer struct {
	alg         jwa.KeyAlgorithm
	key         jwk.Key
	jwksPayload []byte
}

func NewSigner(privateKeys []jwk.Key) (Signer, error) {
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
		alg:         signingAlg,
		key:         signingKey,
		jwksPayload: jwksPayload,
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
