package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
	"strings"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}
	notmanURL := os.Getenv("NOTMAN_URL")
	if notmanURL == "" {
		notmanURL = "http://notman:8080"
	}

	mux := http.NewServeMux()

	mux.HandleFunc("POST /callback", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		slog.Info("Received callback")

		var event struct {
			Data struct {
				SendToken string `json:"sendToken"`
			} `json:"Data"`
		}
		if err := json.Unmarshal(body, &event); err != nil {
			log.Printf("parse error: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if event.Data.SendToken != "" {
			sendURL := fmt.Sprintf("%s/send?send_token=%s", notmanURL, event.Data.SendToken)
			resp, err := http.Post(sendURL, "application/json", strings.NewReader("hello world\n\n"))
			if err != nil {
				slog.Error("Failed to POST message", "error", err)
			} else {
				resp.Body.Close()
				slog.Info("message sent")
			}
		}

		w.WriteHeader(http.StatusOK)
	})

	log.Printf("listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}
