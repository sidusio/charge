package test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
)

// StdoutLogConsumer is a LogConsumer that prints the log to stdout
type StdoutLogConsumer struct {
	prefix string
}

// Accept prints the log to stdout
func (lc StdoutLogConsumer) Accept(l testcontainers.Log) {
	fmt.Printf("[%s] %s", lc.prefix, string(l.Content))
}

func TestGreenFlow(t *testing.T) {
	ctx := context.Background()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	pemBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	keys := []map[string]string{{
		"id":  "test-key",
		"pem": string(pemBytes),
		"alg": "RS256",
	}}
	keysJSON, err := json.Marshal(keys)
	if err != nil {
		t.Fatal(err)
	}

	net, err := network.New(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer net.Remove(ctx)

	backendC, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			FromDockerfile: testcontainers.FromDockerfile{
				Context:    "..",
				Dockerfile: "test/backend/Dockerfile",
			},
			ExposedPorts: []string{"8081/tcp"},
			Networks:     []string{net.Name},
			Hostname:     "backend",
			Env: map[string]string{
				"PORT":       "8081",
				"NOTMAN_URL": "http://notman:8080",
			},
			WaitingFor: wait.ForLog("listening on").WithStartupTimeout(60 * time.Second),
			LogConsumerCfg: &testcontainers.LogConsumerConfig{
				Consumers: []testcontainers.LogConsumer{StdoutLogConsumer{prefix: "backend"}},
			},
		},
		Started: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer backendC.Terminate(ctx)

	notmanC, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "notman:local",
			ExposedPorts: []string{"8080/tcp"},
			Networks:     []string{net.Name},
			Hostname:     "notman",
			Env: map[string]string{
				"NOTMAN_DEPLOYMENT_URL":          "http://notman:8080",
				"NOTMAN_SIGNING_KEYS":            string(keysJSON),
				"NOTMAN_ALLOW_ALL_ORIGINS":       "true",
				"NOTMAN_MAX_CONNECTION_DURATION": "30s",
			},
			WaitingFor: wait.ForHTTP("/.well-known/jwks.json").
				WithPort("8080/tcp").
				WithStartupTimeout(120 * time.Second),
			LogConsumerCfg: &testcontainers.LogConsumerConfig{
				Consumers: []testcontainers.LogConsumer{StdoutLogConsumer{prefix: "notman"}},
			},
		},
		Started: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer notmanC.Terminate(ctx)

	clientC, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			FromDockerfile: testcontainers.FromDockerfile{
				Context:    "..",
				Dockerfile: "test/client/Dockerfile",
			},
			Networks: []string{net.Name},
			Env: map[string]string{
				"NOTMAN_URL":  "http://notman:8080",
				"BACKEND_URL": "http://backend:8081",
			},
			WaitingFor: wait.ForLog("message=").WithStartupTimeout(30 * time.Second),
			LogConsumerCfg: &testcontainers.LogConsumerConfig{
				Consumers: []testcontainers.LogConsumer{StdoutLogConsumer{prefix: "client"}},
			},
		},
		Started: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer clientC.Terminate(ctx)

	clientLogsReader, err := clientC.Logs(ctx)
	require.NoError(t, err)
	defer clientLogsReader.Close()

	clientLogs, err := io.ReadAll(clientLogsReader)
	require.NoError(t, err)
	assert.Contains(t, string(clientLogs), "message=\"hello world\"")

	notmanLogsReader, err := notmanC.Logs(ctx)
	require.NoError(t, err)
	defer notmanLogsReader.Close()

	notmanLogs, err := io.ReadAll(notmanLogsReader)
	require.NoError(t, err)
	assert.NotContains(t, string(notmanLogs), "ERROR")
}
