/*
 * SPDX-FileCopyrightText: © 2017-2026 Istari Digital, Inc.
 * SPDX-License-Identifier: Apache-2.0
 */

package modusgraph

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRetryPolicyDelayExponentialGrowth(t *testing.T) {
	p := RetryPolicy{
		BaseDelay: 100 * time.Millisecond,
		MaxDelay:  10 * time.Second,
		Jitter:    0,
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
	assert.Equal(t, 3*time.Second, p.delay(2))
	assert.Equal(t, 3*time.Second, p.delay(3))
	assert.Equal(t, 3*time.Second, p.delay(10))
}

func TestRetryPolicyDelayWithJitter(t *testing.T) {
	p := RetryPolicy{
		BaseDelay: 100 * time.Millisecond,
		MaxDelay:  10 * time.Second,
		Jitter:    0.5,
	}

	for range 100 {
		d := p.delay(0)
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

	for range 10 {
		assert.Equal(t, 100*time.Millisecond, p.delay(0))
		assert.Equal(t, 200*time.Millisecond, p.delay(1))
	}
}
