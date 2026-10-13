// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

package grpc

import (
	"context"
	stderrors "errors"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/scheduler/schedulertest"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// TestGRPCRateLimited_RoundTrip serves the real scheduler gRPC server and
// checks that a server-side rate_limited error reaches the SDK caller as the
// typed, retryable rate_limited code rather than a generic gRPC status.
func TestGRPCRateLimited_RoundTrip(t *testing.T) {
	tests := []struct {
		name      string
		serverErr error
		wantCode  errors.ErrorCode
	}{
		{
			name:      "rate_limited",
			serverErr: qberrors.New(qberrors.ErrRateLimited, "scheduler.GetJob", "too many requests"),
			wantCode:  errors.CodeRateLimited,
		},
		{
			name:      "resource_exhausted",
			serverErr: qberrors.New(qberrors.ErrResourceExhausted, "scheduler.GetJob", "quota exceeded"),
			wantCode:  errors.CodeResourceExhausted,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jobs := schedulertest.New()
			jobs.Err = tt.serverErr
			client := newSchedulerTransport(t, jobs, nil)

			_, err := client.GetJob(context.Background(), 1)
			if err == nil {
				t.Fatal("GetJob succeeded, want an error")
			}
			if got := errors.Code(err); got != tt.wantCode {
				t.Fatalf("errors.Code(%v) = %v, want %v", err, got, tt.wantCode)
			}
			if !errors.IsRetryable(err) {
				t.Fatalf("errors.IsRetryable(%v) = false, want true", err)
			}
		})
	}
}

func TestResourceExhaustedCode(t *testing.T) {
	tests := []struct {
		message string
		want    errors.ErrorCode
	}{
		{"scheduler.GetJob: rate_limited: too many requests", errors.CodeRateLimited},
		{"rate_limited: too many requests", errors.CodeRateLimited},
		{"scheduler.GetJob: resource_exhausted: quota exceeded", errors.CodeResourceExhausted},
		{"grpc: received message larger than max", errors.CodeResourceExhausted},
		{"op: resource_exhausted: caller was rate_limited", errors.CodeResourceExhausted},
		{"", errors.CodeResourceExhausted},
	}

	for _, tt := range tests {
		if got := resourceExhaustedCode(tt.message); got != tt.want {
			t.Errorf("resourceExhaustedCode(%q) = %v, want %v", tt.message, got, tt.want)
		}
	}
}

func TestTypedError(t *testing.T) {
	t.Run("raw status becomes typed", func(t *testing.T) {
		raw := status.Error(codes.ResourceExhausted, "op: rate_limited: slow down")
		got := typedError(raw)
		if errors.Code(got) != errors.CodeRateLimited {
			t.Fatalf("errors.Code(%v) = %v, want %v", got, errors.Code(got), errors.CodeRateLimited)
		}
	})

	t.Run("server package error becomes typed", func(t *testing.T) {
		apiErr := &qberrors.QBError{
			Code: qberrors.ErrResourceExhausted,
			Op:   "grpc",
			Err:  stderrors.New("scheduler.GetJob: rate_limited: too many requests"),
		}
		got := typedError(apiErr)
		if errors.Code(got) != errors.CodeRateLimited {
			t.Fatalf("errors.Code(%v) = %v, want %v", got, errors.Code(got), errors.CodeRateLimited)
		}
	})

	t.Run("server package error without a wrapped error keeps its code", func(t *testing.T) {
		apiErr := &qberrors.QBError{Code: qberrors.ErrNotFound, Op: "kms.GetKey"}
		got := typedError(apiErr)
		if errors.Code(got) != errors.CodeNotFound {
			t.Fatalf("errors.Code(%v) = %v, want %v", got, errors.Code(got), errors.CodeNotFound)
		}
	})

	t.Run("typed error passes through", func(t *testing.T) {
		typed := errors.New(errors.CodeNotFound, "op", "missing")
		if got := typedError(typed); got != error(typed) {
			t.Fatalf("typedError(%v) = %v, want the same error", typed, got)
		}
	})

	t.Run("non-status error passes through", func(t *testing.T) {
		plain := stderrors.New("dial failed")
		if got := typedError(plain); got != plain {
			t.Fatalf("typedError(%v) = %v, want the same error", plain, got)
		}
	})
}
