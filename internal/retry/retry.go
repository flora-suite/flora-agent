// Package retry provides retry logic with exponential backoff.
package retry

import (
	"context"
	"errors"
	"math"
	"math/rand"
	"time"
)

// Config defines retry behavior.
type Config struct {
	// MaxAttempts is the maximum number of attempts (including the initial attempt).
	MaxAttempts int
	// InitialDelay is the delay before the first retry.
	InitialDelay time.Duration
	// MaxDelay is the maximum delay between retries.
	MaxDelay time.Duration
	// Multiplier is the factor by which the delay increases after each retry.
	Multiplier float64
	// Jitter adds randomness to delays (0.0 to 1.0, where 0.1 = 10% jitter).
	Jitter float64
}

// DefaultConfig returns sensible default retry configuration.
func DefaultConfig() Config {
	return Config{
		MaxAttempts:  5,
		InitialDelay: 1 * time.Second,
		MaxDelay:     60 * time.Second,
		Multiplier:   2.0,
		Jitter:       0.1,
	}
}

// RetryableError wraps an error and indicates whether it should be retried.
type RetryableError struct {
	Err       error
	Retryable bool
}

func (e *RetryableError) Error() string {
	return e.Err.Error()
}

func (e *RetryableError) Unwrap() error {
	return e.Err
}

// Retryable wraps an error to indicate it should be retried.
func Retryable(err error) error {
	if err == nil {
		return nil
	}
	return &RetryableError{Err: err, Retryable: true}
}

// NonRetryable wraps an error to indicate it should NOT be retried.
func NonRetryable(err error) error {
	if err == nil {
		return nil
	}
	return &RetryableError{Err: err, Retryable: false}
}

// IsRetryable checks if an error should be retried.
// By default, errors are considered retryable unless explicitly marked otherwise.
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}

	var retryErr *RetryableError
	if errors.As(err, &retryErr) {
		return retryErr.Retryable
	}

	// By default, treat errors as retryable
	return true
}

// Func is the function type that can be retried.
type Func func(ctx context.Context) error

// Do executes the function with retry logic.
func Do(ctx context.Context, cfg Config, fn Func) error {
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 1
	}

	var lastErr error
	delay := cfg.InitialDelay

	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		// Execute the function
		err := fn(ctx)
		if err == nil {
			return nil
		}

		lastErr = err

		// Check if we should retry
		if !IsRetryable(err) {
			return err
		}

		// Check if this was the last attempt
		if attempt >= cfg.MaxAttempts {
			break
		}

		// Check context before sleeping
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Calculate delay with jitter
		actualDelay := addJitter(delay, cfg.Jitter)

		// Wait before retrying
		timer := time.NewTimer(actualDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}

		// Increase delay for next retry (exponential backoff)
		delay = time.Duration(float64(delay) * cfg.Multiplier)
		if delay > cfg.MaxDelay {
			delay = cfg.MaxDelay
		}
	}

	return lastErr
}

// DoWithResult executes a function that returns a value with retry logic.
func DoWithResult[T any](ctx context.Context, cfg Config, fn func(ctx context.Context) (T, error)) (T, error) {
	var result T
	var lastErr error

	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 1
	}

	delay := cfg.InitialDelay

	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		// Execute the function
		res, err := fn(ctx)
		if err == nil {
			return res, nil
		}

		lastErr = err

		// Check if we should retry
		if !IsRetryable(err) {
			return result, err
		}

		// Check if this was the last attempt
		if attempt >= cfg.MaxAttempts {
			break
		}

		// Check context before sleeping
		if ctx.Err() != nil {
			return result, ctx.Err()
		}

		// Calculate delay with jitter
		actualDelay := addJitter(delay, cfg.Jitter)

		// Wait before retrying
		timer := time.NewTimer(actualDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return result, ctx.Err()
		case <-timer.C:
		}

		// Increase delay for next retry
		delay = time.Duration(float64(delay) * cfg.Multiplier)
		if delay > cfg.MaxDelay {
			delay = cfg.MaxDelay
		}
	}

	return result, lastErr
}

// addJitter adds random jitter to a duration.
func addJitter(d time.Duration, jitterFactor float64) time.Duration {
	if jitterFactor <= 0 {
		return d
	}

	jitter := (rand.Float64()*2 - 1) * jitterFactor * float64(d)
	return time.Duration(math.Max(float64(time.Millisecond), float64(d)+jitter))
}
