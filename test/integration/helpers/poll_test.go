// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//go:build integration

package helpers

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pollTestError is the condition failure the Poll tests inject.
type pollTestError struct{}

func (pollTestError) Error() string { return "poll test failure" }

func TestPollConditionBecomesTrue(t *testing.T) {
	var calls atomic.Int32
	err := Poll(context.Background(), time.Millisecond, time.Second, func(context.Context) (bool, error) {
		return calls.Add(1) == 3, nil
	})

	require.NoError(t, err)
	assert.Equal(t, int32(3), calls.Load())
}

func TestPollDeadlineReturnsTypedError(t *testing.T) {
	err := Poll(context.Background(), time.Millisecond, 20*time.Millisecond, func(context.Context) (bool, error) {
		return false, pollTestError{}
	})

	var timeoutErr *PollTimeoutError
	require.ErrorAs(t, err, &timeoutErr)
	assert.Equal(t, 20*time.Millisecond, timeoutErr.Timeout)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.ErrorIs(t, err, pollTestError{}, "the last condition error is carried")
}

func TestPollCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var calls atomic.Int32
	err := Poll(ctx, time.Millisecond, time.Minute, func(context.Context) (bool, error) {
		calls.Add(1)
		return false, nil
	})

	var timeoutErr *PollTimeoutError
	require.ErrorAs(t, err, &timeoutErr)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, int32(1), calls.Load())
}

func TestPollFinalErrorStopsAtOnce(t *testing.T) {
	var calls atomic.Int32
	err := Poll(context.Background(), time.Millisecond, time.Minute, func(context.Context) (bool, error) {
		calls.Add(1)
		return false, &FinalPollError{Cause: pollTestError{}}
	})

	assert.ErrorIs(t, err, pollTestError{})
	var timeoutErr *PollTimeoutError
	assert.False(t, errors.As(err, &timeoutErr), "a final error is not a timeout")
	assert.Equal(t, int32(1), calls.Load())
}

func TestPollRejectsNonPositiveInterval(t *testing.T) {
	err := Poll(context.Background(), 0, time.Second, func(context.Context) (bool, error) {
		return true, nil
	})

	var intervalErr *PollIntervalError
	require.ErrorAs(t, err, &intervalErr)
	assert.Equal(t, time.Duration(0), intervalErr.Interval)
}
