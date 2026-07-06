package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"time"
)

const (
	DefaultLLMRetryAttempts = 4
	DefaultLLMRetryBackoff  = 2 * time.Second
	DefaultLLMCallTimeout   = 90 * time.Second
)

type RetryOptions struct {
	MaxAttempts       int
	BaseDelay         time.Duration
	PerAttemptTimeout time.Duration
}

type RetryLLM struct {
	base LLM
	opts RetryOptions
}

func NewRetryLLM(base LLM, opts RetryOptions) *RetryLLM {
	if opts.MaxAttempts <= 0 {
		opts.MaxAttempts = 1
	}
	return &RetryLLM{base: base, opts: opts}
}

func (r *RetryLLM) Call(ctx context.Context, req LLMRequest) (*LLMResponse, error) {
	var lastErr error
	attempts := 0
	logPrefix := "[agent] llm retry"
	if traceID := llmTraceID(ctx); traceID != "" {
		logPrefix += " event=" + traceID
	}
	for attempt := 1; attempt <= r.opts.MaxAttempts; attempt++ {
		attempts = attempt
		attemptCtx := ctx
		cancel := func() {}
		if r.opts.PerAttemptTimeout > 0 {
			attemptCtx, cancel = context.WithTimeout(ctx, r.opts.PerAttemptTimeout)
		}

		resp, err := r.base.Call(attemptCtx, req)
		cancel()
		if err == nil {
			if attempt > 1 {
				log.Printf("%s success after %d attempt(s)", logPrefix, attempt)
			}
			return resp, nil
		}
		lastErr = err

		if attempt == r.opts.MaxAttempts || !isRetryableLLMError(err) || ctx.Err() != nil {
			break
		}

		delay := retryDelay(r.opts.BaseDelay, attempt)
		log.Printf("%s scheduling attempt %d/%d after %s: %s", logPrefix, attempt+1, r.opts.MaxAttempts, delay, redactCredentials(err.Error()))
		if delay <= 0 {
			continue
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, fmt.Errorf("after %d attempt(s): %w", attempt, ctx.Err())
		case <-timer.C:
		}
	}

	if attempts > 1 {
		log.Printf("%s failed after %d attempt(s): %s", logPrefix, attempts, redactCredentials(lastErr.Error()))
	}
	return nil, fmt.Errorf("after %d attempt(s): %w", attempts, lastErr)
}

func retryDelay(base time.Duration, attempt int) time.Duration {
	if base <= 0 {
		return 0
	}
	if attempt <= 1 {
		return base
	}
	return base << (attempt - 1)
}

func isRetryableLLMError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "context deadline exceeded"):
		return true
	case strings.Contains(msg, "timeout"):
		return true
	case strings.Contains(msg, "temporary"):
		return true
	case strings.Contains(msg, "eof"):
		return true
	case strings.Contains(msg, "connection reset"):
		return true
	case strings.Contains(msg, "connection refused"):
		return true
	case strings.Contains(msg, "broken pipe"):
		return true
	case strings.Contains(msg, "api error 429"):
		return true
	case strings.Contains(msg, "api error 500"):
		return true
	case strings.Contains(msg, "api error 502"):
		return true
	case strings.Contains(msg, "api error 503"):
		return true
	case strings.Contains(msg, "api error 504"):
		return true
	default:
		return false
	}
}
