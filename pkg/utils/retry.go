package utils

import (
	"context"
	"fmt"
	"time"

	"github.com/Luckyx-Labs/chainscan-sync/internal/config"
	"github.com/Luckyx-Labs/chainscan-sync/pkg/logger"
)

// RetryFunc retry function
type RetryFunc func() error

// Retry execute a function with retry logic
func Retry(ctx context.Context, cfg config.RetryConfig, fn RetryFunc, description string) error {
	var lastErr error
	interval := cfg.InitialInterval

	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		err := fn()
		if err == nil {
			if attempt > 1 {
				logger.Infof("%s succeeded after attempt %d", description, attempt)
			}
			return nil
		}

		lastErr = err
		if attempt < cfg.MaxAttempts {
			logger.Warnf("%s failed (attempt %d/%d): %v, retrying after %v",
				description, attempt, cfg.MaxAttempts, err, interval)

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(interval):
			}

			// Calculate next retry interval
			interval = time.Duration(float64(interval) * cfg.Multiplier)
			if interval > cfg.MaxInterval {
				interval = cfg.MaxInterval
			}
		}
	}

	return fmt.Errorf("%s failed to retry after %d attempts: %w", description, cfg.MaxAttempts, lastErr)
}

// RetryWithResult retry a function with result and return the result
func RetryWithResult[T any](ctx context.Context, cfg config.RetryConfig, fn func() (T, error), description string) (T, error) {
	var result T
	var lastErr error
	interval := cfg.InitialInterval

	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}

		res, err := fn()
		if err == nil {
			if attempt > 1 {
				logger.Infof("%s succeeded after attempt %d", description, attempt)
			}
			return res, nil
		}

		lastErr = err
		if attempt < cfg.MaxAttempts {
			logger.Warnf("%s failed (attempt %d/%d): %v, retrying after %v",
				description, attempt, cfg.MaxAttempts, err, interval)

			select {
			case <-ctx.Done():
				return result, ctx.Err()
			case <-time.After(interval):
			}

			// calculate next retry interval
			interval = time.Duration(float64(interval) * cfg.Multiplier)
			if interval > cfg.MaxInterval {
				interval = cfg.MaxInterval
			}
		}
	}

	return result, fmt.Errorf("%s failed to retry after %d attempts: %w", description, cfg.MaxAttempts, lastErr)
}

// RetryWithBackoff retry with exponential backoff (without description parameter)
func RetryWithBackoff(ctx context.Context, cfg config.RetryConfig, fn RetryFunc) error {
	var lastErr error
	interval := cfg.InitialInterval

	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		err := fn()
		if err == nil {
			return nil
		}

		lastErr = err
		if attempt < cfg.MaxAttempts {
			logger.Debugf("Retry failed (attempt %d/%d): %v, retrying after %v",
				attempt, cfg.MaxAttempts, err, interval)

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(interval):
			}

			// Calculate next retry interval (exponential backoff)
			interval = time.Duration(float64(interval) * cfg.Multiplier)
			if interval > cfg.MaxInterval {
				interval = cfg.MaxInterval
			}
		}
	}

	return fmt.Errorf("failed to retry after %d attempts: %w", cfg.MaxAttempts, lastErr)
}
