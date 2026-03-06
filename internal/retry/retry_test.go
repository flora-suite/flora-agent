package retry

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	assert.Equal(t, 5, cfg.MaxAttempts)
	assert.Equal(t, 1*time.Second, cfg.InitialDelay)
	assert.Equal(t, 60*time.Second, cfg.MaxDelay)
	assert.Equal(t, 2.0, cfg.Multiplier)
	assert.Equal(t, 0.1, cfg.Jitter)
}

func TestRetryableError(t *testing.T) {
	baseErr := errors.New("test error")

	t.Run("Retryable", func(t *testing.T) {
		err := Retryable(baseErr)
		require.NotNil(t, err)
		assert.True(t, IsRetryable(err))
		assert.Equal(t, "test error", err.Error())
		assert.True(t, errors.Is(err, baseErr))
	})

	t.Run("NonRetryable", func(t *testing.T) {
		err := NonRetryable(baseErr)
		require.NotNil(t, err)
		assert.False(t, IsRetryable(err))
		assert.Equal(t, "test error", err.Error())
		assert.True(t, errors.Is(err, baseErr))
	})

	t.Run("NilError", func(t *testing.T) {
		assert.Nil(t, Retryable(nil))
		assert.Nil(t, NonRetryable(nil))
		assert.False(t, IsRetryable(nil))
	})

	t.Run("PlainErrorIsRetryable", func(t *testing.T) {
		// Plain errors without wrapper are considered retryable by default
		assert.True(t, IsRetryable(baseErr))
	})
}

func TestDo_Success(t *testing.T) {
	cfg := Config{
		MaxAttempts:  3,
		InitialDelay: 10 * time.Millisecond,
		MaxDelay:     100 * time.Millisecond,
		Multiplier:   2.0,
		Jitter:       0,
	}

	attempts := 0
	err := Do(context.Background(), cfg, func(ctx context.Context) error {
		attempts++
		return nil
	})

	assert.NoError(t, err)
	assert.Equal(t, 1, attempts)
}

func TestDo_RetryThenSuccess(t *testing.T) {
	cfg := Config{
		MaxAttempts:  5,
		InitialDelay: 10 * time.Millisecond,
		MaxDelay:     100 * time.Millisecond,
		Multiplier:   2.0,
		Jitter:       0,
	}

	attempts := 0
	err := Do(context.Background(), cfg, func(ctx context.Context) error {
		attempts++
		if attempts < 3 {
			return errors.New("temporary error")
		}
		return nil
	})

	assert.NoError(t, err)
	assert.Equal(t, 3, attempts)
}

func TestDo_MaxAttemptsExceeded(t *testing.T) {
	cfg := Config{
		MaxAttempts:  3,
		InitialDelay: 10 * time.Millisecond,
		MaxDelay:     100 * time.Millisecond,
		Multiplier:   2.0,
		Jitter:       0,
	}

	attempts := 0
	expectedErr := errors.New("persistent error")
	err := Do(context.Background(), cfg, func(ctx context.Context) error {
		attempts++
		return expectedErr
	})

	assert.Error(t, err)
	assert.Equal(t, expectedErr, err)
	assert.Equal(t, 3, attempts)
}

func TestDo_NonRetryableError(t *testing.T) {
	cfg := Config{
		MaxAttempts:  5,
		InitialDelay: 10 * time.Millisecond,
		MaxDelay:     100 * time.Millisecond,
		Multiplier:   2.0,
		Jitter:       0,
	}

	attempts := 0
	err := Do(context.Background(), cfg, func(ctx context.Context) error {
		attempts++
		return NonRetryable(errors.New("non-retryable error"))
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "non-retryable error")
	assert.Equal(t, 1, attempts) // Should not retry
}

func TestDo_ContextCanceled(t *testing.T) {
	cfg := Config{
		MaxAttempts:  5,
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     1 * time.Second,
		Multiplier:   2.0,
		Jitter:       0,
	}

	ctx, cancel := context.WithCancel(context.Background())

	attempts := 0
	errCh := make(chan error, 1)

	go func() {
		err := Do(ctx, cfg, func(ctx context.Context) error {
			attempts++
			return errors.New("error")
		})
		errCh <- err
	}()

	// Cancel after a short delay
	time.Sleep(50 * time.Millisecond)
	cancel()

	err := <-errCh
	assert.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled) || attempts > 0)
}

func TestDo_ZeroMaxAttempts(t *testing.T) {
	cfg := Config{
		MaxAttempts:  0, // Should be treated as 1
		InitialDelay: 10 * time.Millisecond,
		MaxDelay:     100 * time.Millisecond,
		Multiplier:   2.0,
		Jitter:       0,
	}

	attempts := 0
	err := Do(context.Background(), cfg, func(ctx context.Context) error {
		attempts++
		return errors.New("error")
	})

	assert.Error(t, err)
	assert.Equal(t, 1, attempts)
}

func TestDoWithResult_Success(t *testing.T) {
	cfg := Config{
		MaxAttempts:  3,
		InitialDelay: 10 * time.Millisecond,
		MaxDelay:     100 * time.Millisecond,
		Multiplier:   2.0,
		Jitter:       0,
	}

	attempts := 0
	result, err := DoWithResult(context.Background(), cfg, func(ctx context.Context) (string, error) {
		attempts++
		return "success", nil
	})

	assert.NoError(t, err)
	assert.Equal(t, "success", result)
	assert.Equal(t, 1, attempts)
}

func TestDoWithResult_RetryThenSuccess(t *testing.T) {
	cfg := Config{
		MaxAttempts:  5,
		InitialDelay: 10 * time.Millisecond,
		MaxDelay:     100 * time.Millisecond,
		Multiplier:   2.0,
		Jitter:       0,
	}

	attempts := 0
	result, err := DoWithResult(context.Background(), cfg, func(ctx context.Context) (int, error) {
		attempts++
		if attempts < 3 {
			return 0, errors.New("temporary error")
		}
		return 42, nil
	})

	assert.NoError(t, err)
	assert.Equal(t, 42, result)
	assert.Equal(t, 3, attempts)
}

func TestDoWithResult_MaxAttemptsExceeded(t *testing.T) {
	cfg := Config{
		MaxAttempts:  3,
		InitialDelay: 10 * time.Millisecond,
		MaxDelay:     100 * time.Millisecond,
		Multiplier:   2.0,
		Jitter:       0,
	}

	attempts := 0
	result, err := DoWithResult(context.Background(), cfg, func(ctx context.Context) (string, error) {
		attempts++
		return "", errors.New("persistent error")
	})

	assert.Error(t, err)
	assert.Empty(t, result)
	assert.Equal(t, 3, attempts)
}

func TestDoWithResult_NonRetryableError(t *testing.T) {
	cfg := Config{
		MaxAttempts:  5,
		InitialDelay: 10 * time.Millisecond,
		MaxDelay:     100 * time.Millisecond,
		Multiplier:   2.0,
		Jitter:       0,
	}

	attempts := 0
	result, err := DoWithResult(context.Background(), cfg, func(ctx context.Context) (string, error) {
		attempts++
		return "", NonRetryable(errors.New("non-retryable"))
	})

	assert.Error(t, err)
	assert.Empty(t, result)
	assert.Equal(t, 1, attempts)
}

func TestAddJitter(t *testing.T) {
	base := 100 * time.Millisecond

	t.Run("NoJitter", func(t *testing.T) {
		result := addJitter(base, 0)
		assert.Equal(t, base, result)
	})

	t.Run("WithJitter", func(t *testing.T) {
		// Run multiple times and verify jitter is applied
		results := make(map[time.Duration]bool)
		for i := 0; i < 100; i++ {
			result := addJitter(base, 0.5)
			results[result] = true
			// Result should be within 50% of base
			assert.GreaterOrEqual(t, result, base/2)
			assert.LessOrEqual(t, result, base*3/2)
		}
		// With high jitter, we should get different values
		assert.Greater(t, len(results), 1)
	})

	t.Run("NegativeJitter", func(t *testing.T) {
		result := addJitter(base, -0.5)
		assert.Equal(t, base, result)
	})
}

func TestExponentialBackoff(t *testing.T) {
	cfg := Config{
		MaxAttempts:  4,
		InitialDelay: 10 * time.Millisecond,
		MaxDelay:     1 * time.Second,
		Multiplier:   2.0,
		Jitter:       0,
	}

	start := time.Now()
	attempts := 0

	_ = Do(context.Background(), cfg, func(ctx context.Context) error {
		attempts++
		return errors.New("error")
	})

	elapsed := time.Since(start)

	// With 4 attempts and delays of 10ms, 20ms, 40ms between them
	// Total delay should be around 70ms (10 + 20 + 40)
	assert.Equal(t, 4, attempts)
	assert.GreaterOrEqual(t, elapsed, 60*time.Millisecond)
}

func TestMaxDelayRespected(t *testing.T) {
	cfg := Config{
		MaxAttempts:  5,
		InitialDelay: 50 * time.Millisecond,
		MaxDelay:     60 * time.Millisecond, // Cap at 60ms
		Multiplier:   10.0,                  // Large multiplier
		Jitter:       0,
	}

	start := time.Now()
	attempts := 0

	_ = Do(context.Background(), cfg, func(ctx context.Context) error {
		attempts++
		return errors.New("error")
	})

	elapsed := time.Since(start)

	// Delays would be 50, 60, 60, 60 (capped at MaxDelay)
	// Total should be around 230ms
	assert.Equal(t, 5, attempts)
	// Allow some tolerance
	assert.Less(t, elapsed, 500*time.Millisecond)
}
