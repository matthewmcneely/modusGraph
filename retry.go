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
	MaxRetries int

	// BaseDelay is the initial delay before the first retry.
	// Subsequent delays grow exponentially: BaseDelay * 2^attempt.
	BaseDelay time.Duration

	// MaxDelay caps the backoff duration. No single delay exceeds this.
	MaxDelay time.Duration

	// Jitter adds randomness to each delay to prevent thundering herd.
	// Expressed as a fraction of the computed delay (e.g. 0.1 = 10%).
	Jitter float64
}

// DefaultRetryPolicy mirrors dgraph4j's defaults:
// 5 retries, 100ms base delay, 5s max delay, 10% jitter.
var DefaultRetryPolicy = RetryPolicy{
	MaxRetries: 10,
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
	for attempt := range policy.MaxRetries + 1 {
		err := fn()
		if err == nil {
			return nil
		}
		if !errors.Is(err, dgo.ErrAborted) || attempt >= policy.MaxRetries {
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
	// Unreachable: the loop runs MaxRetries+1 times and returns on every path.
	panic("unreachable")
}
