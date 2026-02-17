package utils

import (
	"fmt"
	"time"
)

// RetryFunc is any function that returns an error on failure.
type RetryFunc func() error

// WithRetry executes fn up to maxAttempts times, backing off exponentially on each failure.
// It returns the last error if all attempts fail, or nil on success.
func WithRetry(maxAttempts int, baseDelay time.Duration, fn RetryFunc) error {
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		lastErr = fn()
		if lastErr == nil {
			return nil
		}

		if attempt < maxAttempts {
			delay := baseDelay * time.Duration(attempt)
			Logger().Warnf("Attempt %d/%d failed: %v. Retrying in %s...", attempt, maxAttempts, lastErr, delay)
			time.Sleep(delay)
		}
	}
	return fmt.Errorf("all %d attempts failed: %w", maxAttempts, lastErr)
}