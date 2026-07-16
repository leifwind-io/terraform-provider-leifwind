// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"errors"
	"math/rand/v2"
	"net/http"
	"net/url"
	"time"
)

// permanentError marks a failure as deterministic: retrying cannot change
// the outcome (request-body encode, request construction, 2xx decode).
type permanentError struct{ err error }

func (p *permanentError) Error() string { return p.err.Error() }
func (p *permanentError) Unwrap() error { return p.err }

func permanent(err error) error { return &permanentError{err: err} }

// doRetry wraps doOnce: retries transport errors and 5xx for every verb
// (upserts/deletes are idempotent by natural-key design). 4xx never retries.
// A DELETE answered 404 on attempt > 1 is success: the earlier attempt may
// have been executed server-side before the connection failed.
func (c *Client) doRetry(ctx context.Context, method, path string, query url.Values, body, out any) error {
	maxAttempts := c.retry.MaxAttempts
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		err := c.doOnce(ctx, method, path, query, body, out)
		if err == nil {
			return nil
		}
		var apiErr *APIError
		if errors.As(err, &apiErr) {
			if method == http.MethodDelete && attempt > 1 && apiErr.StatusCode == 404 {
				return nil
			}
			if apiErr.StatusCode < 500 {
				return err
			}
		} else {
			var perm *permanentError
			if errors.As(err, &perm) {
				return err
			}
		}
		lastErr = err
		if attempt == maxAttempts {
			break
		}
		if err := sleepBackoff(ctx, c.retry, attempt); err != nil {
			return lastErr
		}
	}
	return lastErr
}

func sleepBackoff(ctx context.Context, rc RetryConfig, attempt int) error {
	backoff := rc.MinBackoff << (attempt - 1)
	if backoff > rc.MaxBackoff || backoff <= 0 {
		backoff = rc.MaxBackoff
	}
	// full jitter; non-cryptographic use, spreads retry timing only
	d := time.Duration(rand.Int64N(int64(backoff) + 1)) //nolint:gosec // jitter, not security-sensitive
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
