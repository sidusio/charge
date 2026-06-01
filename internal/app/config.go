package app

import "log/slog"

type Config struct {
	LogLevel slog.Level `envconfig:"LOG_LEVEL"`
	Port     int        `envconfig:"PORT" default:"8080"`
}
