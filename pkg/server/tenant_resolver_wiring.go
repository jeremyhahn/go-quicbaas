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

package server

import (
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	qbshards "github.com/jeremyhahn/go-quicbaas/pkg/shards"
)

// buildHomeRealmDirectory constructs the home-realm directory over the shared
// platform store, or returns nil when the qrdb client or the platform shard is
// unavailable.
// It is the single construction point shared by the REST login wiring
// (attachHomeRealmResolver) and the gRPC/QUIC tenant-context wiring, so HRD is
// built identically across every transport.
func (s *Server) buildHomeRealmDirectory() *iam.HomeRealmDirectory {
	// The home-realm directory reads the shared platform store directly. HRD
	// is disabled (nil returned) when the qrdb SDK client is not initialized
	// (the server can be started in isolation, e.g. in unit tests) or when
	// the platform shard cannot be resolved.
	if s.qrdbClient == nil {
		return nil
	}
	client, err := s.qrdbClient.ForShard(qbshards.PlatformShardID)
	if err != nil {
		s.logger.WithError(err).Warn("iam: platform shard unavailable; HRD disabled")
		return nil
	}
	shared := newSDKKVStore(client, qbshards.PlatformShardID)
	dir, err := iam.NewHomeRealmDirectory(shared)
	if err != nil {
		s.logger.WithError(err).Warn("iam: failed to build home-realm directory; HRD disabled")
		return nil
	}
	return dir
}
