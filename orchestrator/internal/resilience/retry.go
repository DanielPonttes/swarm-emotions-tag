package resilience

import (
	"context"
	"time"
)

type RetryPolicy struct {
	Attempts  int
	BaseDelay time.Duration
	MaxDelay  time.Duration
}

func (p RetryPolicy) normalized() RetryPolicy {
	if p.Attempts < 1 {
		p.Attempts = 1
	}
	if p.BaseDelay <= 0 {
		p.BaseDelay = 10 * time.Millisecond
	}
	if p.MaxDelay <= 0 {
		p.MaxDelay = 200 * time.Millisecond
	}
	if p.MaxDelay < p.BaseDelay {
		p.MaxDelay = p.BaseDelay
	}
	return p
}

func Do(ctx context.Context, policy RetryPolicy, fn func(context.Context) error) error {
	policy = policy.normalized()
	var lastErr error

	for attempt := 1; attempt <= policy.Attempts; attempt++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		lastErr = fn(ctx)
		if lastErr == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if attempt == policy.Attempts || !isRetryable(lastErr) {
			return lastErr
		}

		delay := backoffDelay(policy, attempt)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return lastErr
}

func DoValue[T any](ctx context.Context, policy RetryPolicy, fn func(context.Context) (T, error)) (T, error) {
	policy = policy.normalized()
	var (
		lastErr error
		value   T
	)
	for attempt := 1; attempt <= policy.Attempts; attempt++ {
		if ctx.Err() != nil {
			return value, ctx.Err()
		}

		value, lastErr = fn(ctx)
		if lastErr == nil {
			return value, nil
		}
		if ctx.Err() != nil {
			return value, ctx.Err()
		}
		if attempt == policy.Attempts || !isRetryable(lastErr) {
			return value, lastErr
		}

		delay := backoffDelay(policy, attempt)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return value, ctx.Err()
		case <-timer.C:
		}
	}
	return value, lastErr
}

func backoffDelay(policy RetryPolicy, attempt int) time.Duration {
	delay := policy.BaseDelay << (attempt - 1)
	if delay > policy.MaxDelay {
		delay = policy.MaxDelay
	}

	// Jitter simples e deterministico para reduzir sincronizacao de retries.
	jitter := time.Duration(time.Now().UnixNano()%int64(policy.BaseDelay)) - (policy.BaseDelay / 2)
	delay += jitter
	if delay < time.Millisecond {
		return time.Millisecond
	}
	return delay
}

func isRetryable(err error) bool {
	return err != nil
}
