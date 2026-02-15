package llm

import (
	"context"
	"errors"
	"math/rand"
	"strings"
	"time"
)

type RetryConfig struct {
	MaxRetries     int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
}

type retryingClient struct {
	inner Client
	cfg   RetryConfig
	rnd   *rand.Rand
}

func WithRetry(inner Client, cfg RetryConfig) Client {
	if inner == nil {
		return inner
	}
	if cfg.MaxRetries <= 0 {
		return inner
	}
	if cfg.InitialBackoff <= 0 {
		cfg.InitialBackoff = 1 * time.Second
	}
	if cfg.MaxBackoff <= 0 {
		cfg.MaxBackoff = 30 * time.Second
	}
	return &retryingClient{
		inner: inner,
		cfg:   cfg,
		rnd:   rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (c *retryingClient) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	var lastErr error

	for attempt := 0; attempt <= c.cfg.MaxRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		resp, err := c.inner.Chat(ctx, req)
		if err == nil {
			return resp, nil
		}

		lastErr = err

		// If we shouldn't retry, return immediately.
		if !isRetryableLLMError(err) {
			return nil, err
		}

		// If this was the last attempt, break and return lastErr below.
		if attempt == c.cfg.MaxRetries {
			break
		}

		sleep := c.backoffForAttempt(attempt)
		timer := time.NewTimer(sleep)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}

	return nil, lastErr
}

func (c *retryingClient) Stream(ctx context.Context, req *ChatRequest) (<-chan StreamChunk, error) {
	// Streaming retry semantics are more complex; delegate directly for now.
	return c.inner.Stream(ctx, req)
}

func (c *retryingClient) ModelInfo() *ModelInfo {
	return c.inner.ModelInfo()
}

func (c *retryingClient) Ping(ctx context.Context) error {
	// Retry Ping once or twice can be useful; keep it simple and delegate.
	return c.inner.Ping(ctx)
}

func (c *retryingClient) Close() error {
	return c.inner.Close()
}

func (c *retryingClient) backoffForAttempt(attempt int) time.Duration {
	// Exponential backoff: base * 2^attempt, capped at MaxBackoff, with +/-20% jitter.
	backoff := c.cfg.InitialBackoff
	for i := 0; i < attempt; i++ {
		backoff *= 2
		if backoff >= c.cfg.MaxBackoff {
			backoff = c.cfg.MaxBackoff
			break
		}
	}
	if backoff <= 0 {
		backoff = 1 * time.Second
	}
	if backoff > c.cfg.MaxBackoff {
		backoff = c.cfg.MaxBackoff
	}

	jitterFrac := (c.rnd.Float64()*0.4 - 0.2) // [-0.2, +0.2]
	jitter := time.Duration(float64(backoff) * jitterFrac)

	sleep := backoff + jitter
	if sleep < 0 {
		sleep = 0
	}
	return sleep
}

func isRetryableLLMError(err error) bool {
	if err == nil {
		return false
	}

	// Never retry on context cancellation/deadline.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	msg := strings.ToLower(err.Error())

	// Heuristic: clients return "API error <code>: ..." for HTTP failures.
	// Don't retry 4xx (auth/validation).
	if strings.Contains(msg, "api error 4") {
		return false
	}
	if strings.Contains(msg, "status 4") {
		return false
	}
	if strings.Contains(msg, " 400") || strings.Contains(msg, " 401") || strings.Contains(msg, " 403") || strings.Contains(msg, " 404") {
		return false
	}

	// Otherwise, assume transient (network, 5xx, relay/proxy flake, etc.).
	return true
}