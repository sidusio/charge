package server_test

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
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
	"sidus.io/charge/server"
)

func Test_Server(t *testing.T) {
	keysJSON := generateChargeKeysBytes(t)

	chargeC, err := testcontainers.Run(t.Context(), "charge:local",
		testcontainers.WithEnv(
			map[string]string{
				"DEPLOYMENT_URL":          "http://localhost:8080",
				"SIGNING_KEYS":            string(keysJSON),
				"ALLOW_INSECURE_ORIGINS":  "true",
				"MAX_CONNECTION_DURATION": "30s",
				"LOG_LEVEL":               "DEBUG",
			},
		),
		testcontainers.WithHostConfigModifier(func(hostConfig *container.HostConfig) {
			hostConfig.NetworkMode = "host"
		}),
		testcontainers.WithLogConsumers(StdOutLogConsumer{}),
	)
	require.NoError(t, err)
	defer chargeC.Terminate(t.Context())

	disconnectCalled := false

	srv := &server.Server{
		AllowedDeploymentIds: []string{"http://localhost:8080"},
		OnConnect: func(ctx context.Context, body server.ConnectBody) error {
			resp, err := http.Post(
				"http://localhost:8080/send?send_token="+body.SendToken,
				"application/json",
				strings.NewReader("hello world"),
			)
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, resp.StatusCode)

			return nil
		},
		OnDisconnect: func(ctx context.Context, body server.DisconnectBody) error {
			disconnectCalled = true
			return nil
		},
	}

	backendMux := http.NewServeMux()
	backendMux.Handle("POST /callback", srv.HandleEvents())
	srv.RegisterChargeAllowed(backendMux, 1800)

	backend := httptest.NewServer(backendMux)
	defer backend.Close()

	waitForURL(t, "http://localhost:8080/.well-known/jwks.json", 5*time.Second)

	sseURL := fmt.Sprintf("http://localhost:8080/sse?token=test&callback_url=%s/callback", backend.URL)

	resp, err := http.Get(sseURL)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	time.AfterFunc(5*time.Second, t.Fail)

	lines := bufio.NewReader(resp.Body)
	for {
		l, _, err := lines.ReadLine()
		if errors.Is(err, io.EOF) {
			t.Fail()
		}
		require.NoError(t, err)
		if string(l) == "data: hello world" {
			break
		}
	}

	resp.Body.Close()
	require.Eventually(t, func() bool {
		return disconnectCalled == true
	}, time.Second, 100*time.Millisecond)

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

func generateChargeKeysBytes(t *testing.T) []byte {
	t.Helper()

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

	return keysJSON
}

type StdOutLogConsumer struct{}

func (StdOutLogConsumer) Accept(log testcontainers.Log) {
	fmt.Print("[charge] " + string(log.Content))
}
