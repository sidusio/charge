// Example backend server demonstrating charge integration.
package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/lestrrat-go/jwx/v4/jwa"
	"github.com/lestrrat-go/jwx/v4/jwk"
	"github.com/lestrrat-go/jwx/v4/jws"
	"github.com/lestrrat-go/jwx/v4/jwt"
	"github.com/tidwall/gjson"
)

//go:embed index.html client.js
var staticFiles embed.FS

var (
	chargeURL   = envWithDefault("CHARGE_URL", "http://localhost:8080")
	callbackURL = envWithDefault("CALLBACK_URL", "http://localhost:8081/callback")
	port        = envWithDefault("PORT", "8081")

	signingSecret = []byte("example-secret-do-not-use-in-production")
)

var connections = make(map[string]string)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	logger.Info("starting example backend", "callbackURL", callbackURL, "chargeURL", chargeURL)

	mux := http.NewServeMux()

	// Endpoint for the web client to get connection details
	mux.HandleFunc("/connect", func(w http.ResponseWriter, r *http.Request) {
		clientToken, err := newClientToken()
		if err != nil {
			logger.Error("failed to create client token", "error", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"client_token": clientToken,
			"charge_url":   chargeURL,
			"callback_url": callbackURL,
		})
	})

	// Client endpoint to post messages
	mux.HandleFunc("POST /sendMessage", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		// Broadcast the message to all connected clients
		for _, sendToken := range connections {
			if err := sendMsg(sendToken, body); err != nil {
				logger.Error("failed to send", "error", err)
			}
		}

		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("/.well-known/charge-allowed", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"allowedDeploymentUrls": []string{chargeURL},
			"cacheDurationSeconds":  1800,
		})
	})

	// Handle callbacks from charge (connected, disconnected, client message)
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			logger.Error("failed to read body", "error", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		// Verify the callback signature using charge's JWKS endpoint
		sig := r.Header.Get("Webhook-Signature")
		if err := verifyCallbackSignature(body, []byte(sig)); err != nil {
			logger.Error("invalid callback signature", "error", err)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		eventType := gjson.GetBytes(body, "type").String()
		logger.Info("event received", "type", eventType, "id", gjson.GetBytes(body, "id").String())

		switch eventType {
		case "charge.connected.v1":
			clientToken := gjson.GetBytes(body, "data.clientToken").String()
			sendToken := gjson.GetBytes(body, "data.sendToken").String()
			connectionID := gjson.GetBytes(body, "data.connectionId").String()
			origin := gjson.GetBytes(body, "data.origin").String()

			// Ensure the request origin is allowed
			err = verifyOrigin(origin)
			if err != nil {
				logger.Error("origin not allowed", "origin", origin, "error", err)
				w.WriteHeader(http.StatusForbidden)
				return
			}

			err = verifyClientToken(clientToken)
			if err != nil {
				logger.Error("client token validation failed", "error", err)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}

			connections[connectionID] = sendToken
			logger.Info("client connected", "connectionId", connectionID)

		case "charge.client.message.v1":
			connectionID := gjson.GetBytes(body, "data.connectionId").String()
			data := gjson.GetBytes(body, "data.data").String()
			logger.Info("client message received", "connectionId", connectionID, "data", data)

			for _, sendToken := range connections {
				if err := sendMsg(sendToken, []byte(data)); err != nil {
					logger.Error("failed to send", "error", err)
				}
			}

		case "charge.disconnected.v1":
			connectionID := gjson.GetBytes(body, "data.connectionId").String()
			delete(connections, connectionID)
			logger.Info("client disconnected", "connectionId", connectionID)
		}

		w.WriteHeader(http.StatusOK)
	})

	// Serve static files for the web client
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.FileServer(http.FS(staticFiles)).ServeHTTP(w, r)
	})

	addr := fmt.Sprintf(":%s", port)
	logger.Info("serving", "url", "http://localhost:"+port)
	err := http.ListenAndServe(addr, mux)
	if err != nil {
		logger.Error("server failed", "error", err)
		os.Exit(1)
	}
}

// sendMsg sends a message to a connected client via charge's /send endpoint using the provided send token.
func sendMsg(sendToken string, body []byte) error {
	resp, err := http.Post(
		fmt.Sprintf("%s/send?send_token=%s", chargeURL, sendToken),
		"text/plain",
		bytes.NewReader(body),
	)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("charge returned %d", resp.StatusCode)
	}

	return nil
}

func newClientToken() (string, error) {
	// This example uses a JWT for the client token, but the backend can use any method to generate a client token
	// as long as it can be verified by the backend when receiving callbacks from charge.
	// The client token should be unique per connection and can contain any information the backend needs to
	// identify the connection (e.g. user ID, permissions, etc).
	token, err := jwt.NewBuilder().
		Issuer("me").
		Subject("web-client").
		Expiration(time.Now().Add(1 * time.Hour)).
		IssuedAt(time.Now()).
		Build()
	if err != nil {
		return "", fmt.Errorf("build token: %w", err)
	}

	signed, err := jwt.Sign(token, jwt.WithKey(jwa.HS256(), signingSecret))
	if err != nil {
		return "", fmt.Errorf("sign token: %w", err)
	}

	return string(signed), nil
}

func verifyOrigin(origin string) error {
	url, err := url.Parse(callbackURL)
	if err != nil {
		return fmt.Errorf("parse callback URL: %w", err)
	}

	// For the sake of the example,
	// we simply check that the origin matches the scheme and host of the callback URL.
	expectedOrigin := url.Scheme + "://" + url.Host

	if origin != expectedOrigin {
		return fmt.Errorf("origin %s does not match expected %s", origin, expectedOrigin)
	}
	return nil
}

func verifyClientToken(tokenStr string) error {
	_, err := jwt.Parse(
		[]byte(tokenStr),
		jwt.WithKey(jwa.HS256(), signingSecret),
		jwt.WithValidate(true),
		jwt.WithIssuer("me"),
		jwt.WithSubject("web-client"),
	)
	return err
}

func verifyCallbackSignature(body, signature []byte) error {
	// Charge exposes a JWKS endpoint that can be used to verify the signature of incoming callbacks.
	resp, err := http.Get(chargeURL + "/.well-known/jwks.json")
	if err != nil {
		return fmt.Errorf("fetch jwks: %w", err)
	}
	defer resp.Body.Close()

	jwksResp, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read jwks: %w", err)
	}
	defer resp.Body.Close()

	keys, err := jwk.Parse(jwksResp)
	if err != nil {
		return fmt.Errorf("parse jwks: %w", err)
	}

	_, err = jws.Verify(signature, jws.WithKeySet(keys), jws.WithDetachedPayload(body))
	return err
}

func envWithDefault(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
