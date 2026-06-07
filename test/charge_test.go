package test

import (
	"bufio"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lestrrat-go/jwx/v4/jwk"
	"github.com/lestrrat-go/jwx/v4/jws"
	"github.com/moby/moby/api/types/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
)

func TestGreenFlow(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	pemBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	keysJSON, err := json.Marshal([]map[string]string{{
		"id":  "test-key",
		"pem": string(pemBytes),
		"alg": "RS256",
	}})
	require.NoError(t, err)

	chargeC, err := testcontainers.Run(t.Context(), "charge:local",
		testcontainers.WithEnv(
			map[string]string{
				"DEPLOYMENT_URL":          "http://localhost:8080",
				"SIGNING_KEYS":            string(keysJSON),
				"ALLOW_ALL_ORIGINS":       "true",
				"MAX_CONNECTION_DURATION": "30s",
			},
		),
		testcontainers.WithHostConfigModifier(func(hostConfig *container.HostConfig) {
			hostConfig.NetworkMode = "host"
		}),
	)
	require.NoError(t, err)
	defer chargeC.Terminate(t.Context())

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)

		resp, err := http.Get("http://localhost:8080/.well-known/jwks.json")
		require.NoError(t, err)

		chargeKeys := jwk.NewSet()
		err = json.NewDecoder(resp.Body).Decode(&chargeKeys)
		require.NoError(t, err)
		require.Equal(t, 1, chargeKeys.Len())

		_, err = jws.Verify([]byte(r.Header.Get("Webhook-signature")), jws.WithKeySet(chargeKeys), jws.WithDetachedPayload(body))
		require.NoError(t, err)

		var event struct {
			Type   string
			Source string
			Data   struct {
				SendToken    string `json:"sendToken"`
				ConnectionId string `json:"connectionId"`
			} `json:"Data"`
		}

		err = json.Unmarshal(body, &event)
		require.NoError(t, err)

		require.Equal(t, "http://localhost:8080", event.Source)
		require.Equal(t, "charge.connected.v1", event.Type)

		go func() {
			resp, err := http.Post(
				"http://localhost:8080/send?send_token="+event.Data.SendToken,
				"application/json",
				strings.NewReader("hello world\n\n"),
			)
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, resp.StatusCode)
		}()

		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	waitForURL(t, "http://localhost:8080/.well-known/jwks.json", 5*time.Second)

	sseURL := fmt.Sprintf("http://localhost:8080/sse?token=test&callback_url=%s/callback", backend.URL)

	resp, err := http.Get(sseURL)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	line, _, err := bufio.NewReader(resp.Body).ReadLine()
	require.NoError(t, err)
	assert.Contains(t, string(line), "hello world")

	chargeLogs, err := chargeC.Logs(t.Context())
	require.NoError(t, err)
	defer chargeLogs.Close()
	logs, _ := io.ReadAll(chargeLogs)
	assert.NotContains(t, string(logs), "ERROR")
}

func waitForURL(t *testing.T, rawURL string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(rawURL)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", rawURL)
}
