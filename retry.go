/*
 * SPDX-FileCopyrightText: © 2017-2026 Istari Digital, Inc.
 * SPDX-License-Identifier: Apache-2.0
 */

package modusgraph

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"

	"github.com/dgraph-io/dgo/v250"
)

// RetryPolicy controls how WithRetry handles aborted transactions.
// Modeled after dgraph4j's RetryPolicy: exponential backoff with jitter.
type RetryPolicy struct {
	// MaxRetries is the maximum number of retry attempts after the initial try.
	// Default: 5.
	MaxRetries int

	// BaseDelay is the initial delay before the first retry.
	// Subsequent delays grow exponentially: BaseDelay * 2^attempt.
	// Default: 100ms.
	BaseDelay time.Duration

	// MaxDelay caps the backoff duration. No single delay will exceed this.
	// Default: 5s.
	MaxDelay time.Duration

	// Jitter adds randomness to each delay to prevent thundering herd.
	// Expressed as a fraction of the computed delay (e.g. 0.1 = 10%).
	// Default: 0.1.
	Jitter float64
}

// DefaultRetryPolicy mirrors dgraph4j's defaults:
// 5 retries, 100ms base delay, 5s max delay, 10% jitter.
var DefaultRetryPolicy = RetryPolicy{
	MaxRetries: 5,
	BaseDelay:  100 * time.Millisecond,
	MaxDelay:   5 * time.Second,
	Jitter:     0.1,
}

// delay computes the backoff duration for a given attempt (0-indexed).
// Formula: min(BaseDelay * 2^attempt, MaxDelay) + random(0, delay * Jitter)
func (p RetryPolicy) delay(attempt int) time.Duration {
	d := p.BaseDelay * time.Duration(1<<uint(attempt))
	if d > p.MaxDelay {
		d = p.MaxDelay
	}
	if p.Jitter > 0 {
		d += time.Duration(float64(d) * p.Jitter * rand.Float64())
	}
	return d
}

// isAbortedError returns true if err (or any error in its chain) is a Dgraph
// transaction abort. This works through pkg/errors wrapping because both
// withStack and withMessage implement Unwrap().
func isAbortedError(err error) bool {
	return errors.Is(err, dgo.ErrAborted)
}

// WithRetry executes fn, retrying on aborted transactions according to policy.
//
// This is an opt-in mechanism modeled after dgraph4j's client.withRetry().
// The caller wraps their mutation logic in fn; WithRetry handles creating
// fresh attempts with exponential backoff when Dgraph returns a transaction
// abort due to concurrent conflicts.
//
// fn is called at least once. On each aborted-transaction error, WithRetry
// waits according to the policy's backoff schedule and calls fn again, up to
// policy.MaxRetries additional times. Non-abort errors are returned immediately.
//
// The context is checked between retries; if cancelled during a backoff sleep,
// the context error is returned.
//
// Usage:
//
//	err := client.WithRetry(ctx, modusgraph.DefaultRetryPolicy, func() error {
//	    return client.Insert(ctx, &entity)
//	})
func (c client) WithRetry(ctx context.Context, policy RetryPolicy, fn func() error) error {
	var lastErr error
	for attempt := 0; attempt <= policy.MaxRetries; attempt++ {
		err := fn()
		if err == nil {
			return nil
		}
		lastErr = err
		if !isAbortedError(err) || attempt >= policy.MaxRetries {
			return err
		}
		d := policy.delay(attempt)
		c.logger.V(1).Info("Transaction aborted, retrying",
			"attempt", attempt+1, "maxRetries", policy.MaxRetries, "delay", d)
		select {
		case <-time.After(d):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return lastErr // unreachable, but satisfies the compiler
}
