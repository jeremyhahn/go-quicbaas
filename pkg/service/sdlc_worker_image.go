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

package service

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc/workerimage"
)

// =============================================================================
// The worker image — refresh on request, status, and the rebinding write
// =============================================================================
//
// The refresh itself is pkg/sdlc/workerimage's; this is its service frontage.
// Two verbs face the transports (RefreshWorkerImage, WorkerImageStatus) and
// one faces the refresher (ReplaceBindingImage), so a promotion is a write
// through this service — validated like every binding write — rather than an
// edit of a store record behind its back.

// WorkerImageRefresher is the refresh loop as the service reaches it;
// *workerimage.Refresher satisfies it.
type WorkerImageRefresher interface {
	RequestRefresh() error
	Status() workerimage.Status
}

// workerImageHolder boxes the refresher for the atomic pointer.
type workerImageHolder struct {
	refresher WorkerImageRefresher
}

var _ workerimage.Bindings = (*SDLCService)(nil)

// SetWorkerImageRefresher attaches the refresh loop. Until one is attached a
// requested refresh is refused and the status reports the loop not serving.
func (s *SDLCService) SetWorkerImageRefresher(refresher WorkerImageRefresher) {
	if refresher == nil {
		s.workerImage.Store(nil)
		return
	}
	s.workerImage.Store(&workerImageHolder{refresher: refresher})
}

// RefreshWorkerImage queues a full refresh pass and returns the status it
// left behind. The pass runs on the daemon's refresh loop, not on the
// caller's request: a build takes minutes, and a request that waited on it
// would outlive every transport's deadline. Poll WorkerImageStatus for the
// outcome.
func (s *SDLCService) RefreshWorkerImage(_ context.Context) (workerimage.Status, error) {
	held := s.workerImage.Load()
	if held == nil {
		return workerimage.Status{}, &workerimage.RefresherNotRunningError{}
	}
	if err := held.refresher.RequestRefresh(); err != nil {
		return held.refresher.Status(), err
	}
	return held.refresher.Status(), nil
}

// WorkerImageStatus reports the refresh loop's state and its last pass. A
// node with no loop reports one that is not serving.
func (s *SDLCService) WorkerImageStatus(_ context.Context) (workerimage.Status, error) {
	held := s.workerImage.Load()
	if held == nil {
		return workerimage.Status{}, nil
	}
	return held.refresher.Status(), nil
}
