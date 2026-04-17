/*
 * SPDX-FileCopyrightText: © 2017-2026 Istari Digital, Inc.
 * SPDX-License-Identifier: Apache-2.0
 */

package modusgraph

import (
	"errors"
	"testing"
	"time"

	"github.com/dgraph-io/dgo/v250"
	"github.com/stretchr/testify/assert"
)

func TestRetryPolicyDelayExponentialGrowth(t *testing.T) {
	p := RetryPolicy{
		BaseDelay: 100 * time.Millisecond,
		MaxDelay:  10 * time.Second,
		Jitter:    0, // deterministic
	}

	assert.Equal(t, 100*time.Millisecond, p.delay(0))
	assert.Equal(t, 200*time.Millisecond, p.delay(1))
	assert.Equal(t, 400*time.Millisecond, p.delay(2))
	assert.Equal(t, 800*time.Millisecond, p.delay(3))
	assert.Equal(t, 1600*time.Millisecond, p.delay(4))
}

func TestRetryPolicyDelayMaxCap(t *testing.T) {
	p := RetryPolicy{
		BaseDelay: 1 * time.Second,
		MaxDelay:  3 * time.Second,
		Jitter:    0,
	}

	assert.Equal(t, 1*time.Second, p.delay(0))
	assert.Equal(t, 2*time.Second, p.delay(1))
	assert.Equal(t, 3*time.Second, p.delay(2)) // capped
	assert.Equal(t, 3*time.Second, p.delay(3)) // still capped
	assert.Equal(t, 3*time.Second, p.delay(10))
}

func TestRetryPolicyDelayWithJitter(t *testing.T) {
	p := RetryPolicy{
		BaseDelay: 100 * time.Millisecond,
		MaxDelay:  10 * time.Second,
		Jitter:    0.5, // up to 50% extra
	}

	// Run many times to verify jitter is within expected bounds.
	for range 100 {
		d := p.delay(0)
		// Base is 100ms, jitter adds up to 50ms.
		assert.GreaterOrEqual(t, d, 100*time.Millisecond, "delay should be at least base")
		assert.LessOrEqual(t, d, 150*time.Millisecond, "delay should not exceed base + 50% jitter")
	}
}

func TestRetryPolicyDelayZeroJitter(t *testing.T) {
	p := RetryPolicy{
		BaseDelay: 100 * time.Millisecond,
		MaxDelay:  10 * time.Second,
		Jitter:    0,
	}

	// Without jitter, delay should be exactly deterministic.
	for range 10 {
		assert.Equal(t, 100*time.Millisecond, p.delay(0))
		assert.Equal(t, 200*time.Millisecond, p.delay(1))
	}
}

func TestIsAbortedError(t *testing.T) {
	assert.True(t, isAbortedError(dgo.ErrAborted))
	assert.False(t, isAbortedError(errors.New("some other error")))
	assert.False(t, isAbortedError(nil))
}

func TestIsAbortedErrorWrapped(t *testing.T) {
	wrapped := errors.Join(errors.New("context"), dgo.ErrAborted)
	assert.True(t, isAbortedError(wrapped))
}
