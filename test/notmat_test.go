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

	notmanC, err := testcontainers.Run(t.Context(), "notman:local",
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
	defer notmanC.Terminate(t.Context())

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var event struct {
			Data struct {
				SendToken string `json:"sendToken"`
			} `json:"Data"`
		}
		if err := json.Unmarshal(body, &event); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if event.Data.SendToken != "" {
			resp, err := http.Post(
				"http://localhost:8080/send?send_token="+event.Data.SendToken,
				"application/json",
				strings.NewReader("hello world\n\n"),
			)
			if err == nil {
				resp.Body.Close()
			}
		}
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

	notmanLogs, err := notmanC.Logs(t.Context())
	require.NoError(t, err)
	defer notmanLogs.Close()
	logs, _ := io.ReadAll(notmanLogs)
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
