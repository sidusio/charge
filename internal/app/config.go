package app

import "log/slog"

type Config struct {
	LogLevel             slog.Level `envconfig:"LOG_LEVEL"`
	Port                 int        `envconfig:"PORT" default:"8080"`
	DeploymentIdentifier string     `envconfig:"DEPLOYMENT_IDENTIFIER"`
	AllowlistedIssuers   []string   `envconfig:"ALLOWLISTED_ISSUERS"`
}
