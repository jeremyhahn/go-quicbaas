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

// sharded_client_detached_test.go holds the detached ShardedClient helper.
// It carries no build tag so every build of the package's tests, the
// integration-tagged vet included, compiles it.

package server

// newDetachedShardedClient returns a ShardedClient with no embedded node
// client attached, for tests that exercise server wiring without a node.
func newDetachedShardedClient() *ShardedClient {
	return &ShardedClient{}
}
