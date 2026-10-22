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
	"time"
)

// PollCondition reports whether the awaited state holds. A non-nil error is
// remembered as the last failure and polling goes on, unless the error is a
// *FinalPollError, which stops Poll at once.
type PollCondition func(ctx context.Context) (bool, error)

// PollTimeoutError reports a Poll whose condition did not hold before its
// timeout passed or its context was cancelled.
type PollTimeoutError struct {
	// Timeout is the bound Poll was given.
	Timeout time.Duration

	// Reason is the context error that stopped polling.
	Reason error

	// LastErr is the last error the condition returned, if any.
	LastErr error
}

func (e *PollTimeoutError) Error() string {
	msg := "condition not met within " + e.Timeout.String()
	if e.Reason != nil {
		msg += " (" + e.Reason.Error() + ")"
	}
	if e.LastErr != nil {
		msg += ": " + e.LastErr.Error()
	}
	return msg
}

// Unwrap exposes both the context error and the last condition error to
// errors.Is and errors.As.
func (e *PollTimeoutError) Unwrap() []error { return []error{e.Reason, e.LastErr} }

// PollIntervalError reports a non-positive polling interval.
type PollIntervalError struct {
	Interval time.Duration
}

func (e *PollIntervalError) Error() string {
	return "poll interval must be positive, got " + e.Interval.String()
}

// FinalPollError marks a condition error that no retry can cure.
type FinalPollError struct {
	Cause error
}

func (e *FinalPollError) Error() string {
	if e.Cause == nil {
		return "final poll error"
	}
	return e.Cause.Error()
}

func (e *FinalPollError) Unwrap() error { return e.Cause }

// Poll evaluates cond at once and then every interval until it holds, cond
// returns a *FinalPollError, or timeout passes. It waits on a ticker and the
// context, never sleeping.
func Poll(ctx context.Context, interval, timeout time.Duration, cond PollCondition) error {
	if interval <= 0 {
		return &PollIntervalError{Interval: interval}
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var lastErr error
	for {
		done, err := cond(ctx)
		if err != nil {
			var final *FinalPollError
			if errors.As(err, &final) {
				return err
			}
			lastErr = err
		} else if done {
			return nil
		}

		select {
		case <-ctx.Done():
			return &PollTimeoutError{Timeout: timeout, Reason: ctx.Err(), LastErr: lastErr}
		case <-ticker.C:
		}
	}
}
