// Пакет slack — интеграция Slack для канального движка по модели «своё
// приложение»: администратор воркспейса вставляет bot- и app-level токены,
// каждая установка получает собственное Socket Mode-соединение.
package slack

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type installConfig struct {
	AppID             string `json:"app_id"`
	TeamID            string `json:"team_id,omitempty"`
	BotUserID         string `json:"bot_user_id,omitempty"`
	BotTokenEncrypted string `json:"bot_token_encrypted"`
	AppTokenEncrypted string `json:"app_token_encrypted,omitempty"`
}

type credentials struct {
	TeamID    string
	BotUserID string
	BotToken  string
}

type Decrypter func(ciphertext []byte) (plaintext []byte, err error)

func decodeCredentials(raw json.RawMessage, decrypt Decrypter) (credentials, error) {
	if len(raw) == 0 {
		return credentials{}, errors.New("slack: empty installation config")
	}
	var cfg installConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return credentials{}, fmt.Errorf("decode slack installation config: %w", err)
	}
	botToken, err := decryptToken(cfg.BotTokenEncrypted, decrypt)
	if err != nil {
		return credentials{}, fmt.Errorf("decrypt bot token: %w", err)
	}
	teamID := cfg.TeamID
	if teamID == "" {
		teamID = cfg.AppID
	}
	return credentials{
		TeamID:    teamID,
		BotUserID: cfg.BotUserID,
		BotToken:  botToken,
	}, nil
}

type PublicConfig struct {
	AppID     string
	TeamID    string
	BotUserID string
}

func DecodePublicConfig(raw json.RawMessage) PublicConfig {
	var cfg installConfig
	_ = json.Unmarshal(raw, &cfg)
	teamID := cfg.TeamID
	if teamID == "" {
		teamID = cfg.AppID
	}
	return PublicConfig{AppID: cfg.AppID, TeamID: teamID, BotUserID: cfg.BotUserID}
}

func decryptToken(enc string, decrypt Decrypter) (string, error) {
	if enc == "" {
		return "", nil
	}
	ciphertext, err := base64.StdEncoding.DecodeString(stripWhitespace(enc))
	if err != nil {
		return "", fmt.Errorf("base64 decode: %w", err)
	}
	if decrypt == nil {
		return string(ciphertext), nil
	}
	plaintext, err := decrypt(ciphertext)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

func stripWhitespace(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case ' ', '\t', '\n', '\r':
			continue
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
