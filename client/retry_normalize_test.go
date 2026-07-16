// SPDX-License-Identifier: MPL-2.0

package client

import (
	"testing"
	"time"
)

func TestRetryConfigNormalization(t *testing.T) {
	t.Parallel()
	const maxJitter = time.Duration(1<<63 - 2)
	c, err := New("http://localhost:0", WithRetry(RetryConfig{
		MaxAttempts: 0,
		MinBackoff:  time.Duration(1<<63 - 1),
		MaxBackoff:  time.Duration(1<<63 - 1),
	}))
	if err != nil {
		t.Fatal(err)
	}
	if c.retry.MaxAttempts != 1 {
		t.Errorf("MaxAttempts = %d, want 1", c.retry.MaxAttempts)
	}
	if c.retry.MinBackoff != maxJitter || c.retry.MaxBackoff != maxJitter {
		t.Errorf("backoff bounds = %v/%v, want both clamped to %v", c.retry.MinBackoff, c.retry.MaxBackoff, maxJitter)
	}
}
