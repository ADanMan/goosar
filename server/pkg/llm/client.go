// Пакет llm — тонкая обёртка над официальным Go SDK OpenAI: единая типизированная
// точка входа для простых обращений к LLM без полного runtime агента
// (заголовок чата, черновик задачи).
package llm

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/ssestream"
	"github.com/openai/openai-go/v3/shared"
)

const FallbackModel = "gpt-4o-mini"

const defaultRequestTimeout = 60 * time.Second

var ErrNotConfigured = errors.New("llm: no API key or base URL configured")

var ErrMissingBaseURL = errors.New("llm: GOOSAR_LLM_API_KEY is set but GOOSAR_LLM_BASE_URL is empty — refusing to send chat text to the SDK default (api.openai.com). Set GOOSAR_LLM_BASE_URL explicitly (e.g. https://api.openai.com/v1) to enable the LLM layer")

type Config struct {
	APIKey string

	BaseURL string

	DefaultModel string

	MaxRetries int

	HTTPClient option.HTTPClient
}

type Client struct {
	sdk          openai.Client
	defaultModel string
	enabled      bool

	configErr error
}

func New(cfg Config) *Client {
	key := strings.TrimSpace(cfg.APIKey)
	base := strings.TrimSpace(cfg.BaseURL)

	defaultModel := strings.TrimSpace(cfg.DefaultModel)
	if defaultModel == "" {
		defaultModel = FallbackModel
	}

	if key != "" && base == "" {
		slog.Error("llm: layer disabled — refusing to use the SDK default upstream", "error", ErrMissingBaseURL)
		return &Client{defaultModel: defaultModel, configErr: ErrMissingBaseURL}
	}

	opts := make([]option.RequestOption, 0, 4)
	if key != "" {
		opts = append(opts, option.WithAPIKey(key))
	}
	if base != "" {
		opts = append(opts, option.WithBaseURL(base))
	}
	if cfg.MaxRetries != 0 {
		retries := cfg.MaxRetries
		if retries < 0 {
			retries = 0
		}
		opts = append(opts, option.WithMaxRetries(retries))
	}
	if cfg.HTTPClient != nil {
		opts = append(opts, option.WithHTTPClient(cfg.HTTPClient))
	}

	return &Client{
		sdk:          openai.NewClient(opts...),
		defaultModel: defaultModel,

		enabled: base != "",
	}
}

func (c *Client) Enabled() bool { return c != nil && c.enabled }

func (c *Client) disabledErr() error {
	if c == nil {
		return ErrNotConfigured
	}
	if c.configErr != nil {
		return c.configErr
	}
	if !c.enabled {
		return ErrNotConfigured
	}
	return nil
}

func (c *Client) DefaultModel() string { return c.defaultModel }

func (c *Client) applyDefaultModel(params *openai.ChatCompletionNewParams) {
	if strings.TrimSpace(string(params.Model)) == "" {
		params.Model = shared.ChatModel(c.defaultModel)
	}
}

func (c *Client) Chat(ctx context.Context, params openai.ChatCompletionNewParams) (*openai.ChatCompletion, error) {
	if err := c.disabledErr(); err != nil {
		return nil, err
	}
	c.applyDefaultModel(&params)

	ctx, cancel := withDefaultTimeout(ctx)
	defer cancel()

	return c.sdk.Chat.Completions.New(ctx, params)
}

func (c *Client) ChatStream(ctx context.Context, params openai.ChatCompletionNewParams) (*ssestream.Stream[openai.ChatCompletionChunk], error) {
	if err := c.disabledErr(); err != nil {
		return nil, err
	}
	c.applyDefaultModel(&params)
	return c.sdk.Chat.Completions.NewStreaming(ctx, params), nil
}

func (c *Client) GenerateText(ctx context.Context, model, systemPrompt, userPrompt string) (string, error) {
	if err := c.disabledErr(); err != nil {
		return "", err
	}

	messages := make([]openai.ChatCompletionMessageParamUnion, 0, 2)
	if strings.TrimSpace(systemPrompt) != "" {
		messages = append(messages, openai.SystemMessage(systemPrompt))
	}
	messages = append(messages, openai.UserMessage(userPrompt))

	params := openai.ChatCompletionNewParams{
		Messages: messages,
		Model:    shared.ChatModel(strings.TrimSpace(model)),
	}

	completion, err := c.Chat(ctx, params)
	if err != nil {
		return "", err
	}
	if len(completion.Choices) == 0 {
		return "", errors.New("llm: upstream returned no choices")
	}
	return completion.Choices[0].Message.Content, nil
}

func withDefaultTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, defaultRequestTimeout)
}

var _ option.HTTPClient = (*http.Client)(nil)
