package main

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"
)

func main() {
	notmanURL := os.Getenv("NOTMAN_URL")
	if notmanURL == "" {
		notmanURL = "http://notman:8080"
	}
	backendURL := os.Getenv("BACKEND_URL")
	if backendURL == "" {
		backendURL = "http://backend:8081"
	}

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{})))

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	reqURL := fmt.Sprintf("%s/sse?token=test&callback_url=%s/callback", notmanURL, backendURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		slog.Error("Failed to create request", "error", err)
	}

	slog.Info("Calling SSE")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Error("Failed to connect", "error", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Error("Received non-200 status code", "code", resp.StatusCode)
	}

	slog.Info("Starting to read SSE")
	r := bufio.NewReader(resp.Body)

	line, isPrefix, err := r.ReadLine()
	if err != nil {
		slog.Error("Couldn't read line", "error", err)
		return
	}
	if isPrefix {
		slog.Error("Couldn't read entire line")
		return
	}

	slog.Info("Received message", "message", string(line))
}
