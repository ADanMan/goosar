package daemon

import (
	"encoding/json"
	"log/slog"

	"github.com/adanman/goosar/server/internal/daemon/execenv"
)

type openclawRuntimeConfig struct {
	Mode    string                `json:"mode"`
	Gateway openclawGatewayFields `json:"gateway"`
}

type openclawGatewayFields struct {
	Host  string `json:"host"`
	Port  int    `json:"port"`
	Token string `json:"token"`
	TLS   bool   `json:"tls"`
}

func decodeOpenclawRuntimeConfig(raw json.RawMessage, logger *slog.Logger) (string, execenv.OpenclawGatewayPin) {
	if len(raw) == 0 {
		return "", execenv.OpenclawGatewayPin{}
	}

	var cfg openclawRuntimeConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		logger.Warn("openclaw runtime_config: parse failed; falling back to local mode", "error", err)
		return "", execenv.OpenclawGatewayPin{}
	}

	switch cfg.Mode {
	case "", "local":
		return cfg.Mode, execenv.OpenclawGatewayPin{}
	case "gateway":
		return cfg.Mode, execenv.OpenclawGatewayPin{
			Host:  cfg.Gateway.Host,
			Port:  cfg.Gateway.Port,
			Token: cfg.Gateway.Token,
			TLS:   cfg.Gateway.TLS,
		}
	default:

		logger.Warn("openclaw runtime_config: unrecognized mode; falling back to local mode",
			"mode", cfg.Mode)
		return cfg.Mode, execenv.OpenclawGatewayPin{}
	}
}
