package mail

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// defaultResendBaseURL — продовый эндпоинт Resend; тесты подменяют его
// httptest.Server.URL через ResendSender.BaseURL напрямую (поле экспортировано
// ровно для этого, никакого отдельного конструктора для тестов не нужно).
const defaultResendBaseURL = "https://api.resend.com"

// ResendSender — транспорт почты поверх HTTP API Resend (T-029): один вызов
// POST /emails с ключом в Authorization: Bearer. Своя реализация, не SDK —
// API Resend это ровно один эндпоинт с плоским JSON, обёртка вокруг
// net/http достаточна и не тянет стороннюю зависимость (см.
// server2/docs/adr/0001-stack.md за общую линию этой сессии на не
// плодить внешние SDK там, где хватает net/http).
type ResendSender struct {
	APIKey  string
	From    string // "Имя <email>" или просто email — components/schemas значения from не описывает, формат Resend
	BaseURL string
	Client  *http.Client
}

// NewResendSender собирает отправитель с 15-секундным таймаутом по умолчанию.
func NewResendSender(apiKey, from string) *ResendSender {
	return &ResendSender{
		APIKey:  apiKey,
		From:    from,
		BaseURL: defaultResendBaseURL,
		Client:  &http.Client{Timeout: 15 * time.Second},
	}
}

type resendSendRequest struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	Text    string   `json:"text"`
}

type resendErrorBody struct {
	Message string `json:"message"`
}

func (s *ResendSender) Send(ctx context.Context, msg Message) error {
	if s.APIKey == "" {
		return errors.New("mail: resend: RESEND_API_KEY не настроен")
	}
	payload, err := json.Marshal(resendSendRequest{From: s.From, To: []string{msg.To}, Subject: msg.Subject, Text: msg.Body})
	if err != nil {
		return fmt.Errorf("mail: resend: сборка тела запроса: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.BaseURL+"/emails", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("mail: resend: сборка запроса: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.APIKey)

	client := s.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("mail: resend: запрос: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if resp.StatusCode >= 300 {
		var eb resendErrorBody
		_ = json.Unmarshal(body, &eb)
		if eb.Message != "" {
			return fmt.Errorf("mail: resend: %s (статус %d)", eb.Message, resp.StatusCode)
		}
		return fmt.Errorf("mail: resend: статус %d: %s", resp.StatusCode, string(body))
	}
	return nil
}
