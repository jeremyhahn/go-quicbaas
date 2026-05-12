// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

package orchestration

import (
	"context"
	"sync/atomic"
)

// =============================================================================
// host_affinity_strict context propagation
// =============================================================================
//
// The "strict" bit travels alongside the host-affinity hint from the public
// REST/gRPC handler down into the RemoteBackendResolver via context, mirroring
// the pattern used for HostAffinity and GpuRequired. Keeping it on the context
// avoids changing the RemoteBackendResolver signature, which would force the
// orchestrator core to learn about strictness — that file is intentionally
// kept stable.

// hostAffinityStrictKey is the unexported context key carrying the per-request
// strict bit. Resolvers OR this with the operator-level default to decide
// whether to fall back to any agent or surface NoMatchingAgentError.
type hostAffinityStrictKey struct{}

// WithHostAffinityStrict returns a context that signals the resolver must
// treat the request's host_affinity hint as a hard requirement — failing
// with NoMatchingAgentError when the named host is offline. Callers normally
// set this from the request's HostAffinityStrict field.
func WithHostAffinityStrict(ctx context.Context) context.Context {
	return context.WithValue(ctx, hostAffinityStrictKey{}, true)
}

// HostAffinityStrictFromContext reports whether the per-request strict flag
// is set on ctx. Operators can also configure a default via
// orchestration.host_affinity.mode = "hard"; resolvers OR these two sources.
func HostAffinityStrictFromContext(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	v, _ := ctx.Value(hostAffinityStrictKey{}).(bool)
	return v
}

// =============================================================================
// ResolveErrorHolder — bubble typed resolver errors up to the REST handler
// =============================================================================
//
// The RemoteBackendResolver interface returns (AgentBackend, bool); when it
// returns (nil, false) the dispatcher falls through to the local backend
// table, which usually surfaces BackendNotFoundError. That mapping is wrong
// for "host offline + strict mode": we want to return the resolver-side
// NoMatchingAgentError verbatim. Rather than widen RemoteBackendResolver
// (which would touch the orchestrator core), we install a thread-safe error
// holder on the context. The resolver writes to it; the public-API handler
// reads it after the call completes and substitutes the holder's error when
// non-nil.

// ResolveErrorHolder is an atomic-pointer error slot. It is small, mutable,
// and lives on a context — never copied or shared across goroutines except
// via the standard context propagation rules. Allocating a holder per
// request is intentional: one request, one resolution outcome.
type ResolveErrorHolder struct {
	// err is loaded/stored atomically as a *resolveErrorBox so the holder
	// stays lock-free on the hot success path (where it is never written).
	err atomic.Pointer[resolveErrorBox]
}

// resolveErrorBox wraps an error so the atomic pointer can carry a typed
// nil-vs-set distinction without losing the original interface value.
type resolveErrorBox struct {
	err error
}

// resolveErrorHolderKey is the unexported context key for the holder.
type resolveErrorHolderKey struct{}

// NewResolveErrorHolder returns an empty holder ready to be threaded onto a
// request context via WithResolveErrorHolder.
func NewResolveErrorHolder() *ResolveErrorHolder {
	return &ResolveErrorHolder{}
}

// WithResolveErrorHolder attaches the holder to ctx so resolvers downstream
// can record a typed error that the calling handler will read post-call.
// Returns the derived context. Callers retain a reference to the holder.
func WithResolveErrorHolder(ctx context.Context, holder *ResolveErrorHolder) context.Context {
	if holder == nil {
		return ctx
	}
	return context.WithValue(ctx, resolveErrorHolderKey{}, holder)
}

// ResolveErrorHolderFromContext returns the holder previously installed via
// WithResolveErrorHolder, or nil if none.
func ResolveErrorHolderFromContext(ctx context.Context) *ResolveErrorHolder {
	if ctx == nil {
		return nil
	}
	h, _ := ctx.Value(resolveErrorHolderKey{}).(*ResolveErrorHolder)
	return h
}

// Set records err on the holder. The first non-nil Set wins; subsequent
// writes are dropped so the original constraint detail (e.g. "host_affinity")
// is preserved through any later filter passes.
func (h *ResolveErrorHolder) Set(err error) {
	if h == nil || err == nil {
		return
	}
	h.err.CompareAndSwap(nil, &resolveErrorBox{err: err})
}

// Err returns the recorded error or nil if none was set.
func (h *ResolveErrorHolder) Err() error {
	if h == nil {
		return nil
	}
	box := h.err.Load()
	if box == nil {
		return nil
	}
	return box.err
}

// StashResolveError is a convenience for resolvers: write err onto the
// context-bound holder, if any. No-op when the context carries no holder.
func StashResolveError(ctx context.Context, err error) {
	if h := ResolveErrorHolderFromContext(ctx); h != nil {
		h.Set(err)
	}
}
