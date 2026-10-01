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

// This file re-exports the go-qrdb/sdk/go runtime peer-discovery and
// cluster-formation primitives so that downstream consumers (e.g.
// go-trusted-platform) reach them through the go-quicbaas SDK alone, without
// importing go-qrdb directly.
//
// These are the RUNTIME discovery primitives (interfaces, constructors,
// manager). They are distinct from:
//   - the quicbaas server-config aliases in server.go (DiscoveryConfig,
//     MulticastDiscoveryConfig — the YAML config layer), and
//   - the *service* registry exposed by Client.Registry() (client/registry),
//     which is unrelated: it lists application service endpoints, not peers.
//
// To avoid clashing with the server-config layer, the runtime discovery
// config types carry a Discovery prefix.

package quicbaas

import (
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// ---------------------------------------------------------------------------
// Runtime discovery primitives (from go-qrdb/sdk/go types_discovery.go)
// ---------------------------------------------------------------------------

// Peer is a discovered cluster peer.
type Peer = qrdbsdk.Peer

// DiscoveryBackend is the behavior implemented by every runtime discovery
// method.
type DiscoveryBackend = qrdbsdk.DiscoveryBackend

// DiscoveryRuntimeConfig is the shared runtime discovery configuration. It is
// named DiscoveryRuntimeConfig (not DiscoveryConfig) to avoid clashing with
// the quicbaas server-config alias in server.go.
type DiscoveryRuntimeConfig = qrdbsdk.DiscoveryConfig

// DiscoveryStaticConfig configures static (fixed peer-list) discovery.
type DiscoveryStaticConfig = qrdbsdk.DiscoveryStaticConfig

// DiscoveryMulticastConfig configures multicast (LAN announcement) discovery.
type DiscoveryMulticastConfig = qrdbsdk.DiscoveryMulticastConfig

// DiscoveryDNSConfig configures DNS SRV-record discovery.
type DiscoveryDNSConfig = qrdbsdk.DiscoveryDNSConfig

// DiscoveryTokenConfig configures token-server (HTTP) discovery.
type DiscoveryTokenConfig = qrdbsdk.DiscoveryTokenConfig

// DiscoveryManager fans out across multiple discovery methods.
type DiscoveryManager = qrdbsdk.DiscoveryManager

// DiscoveryManagerConfig configures a DiscoveryManager.
type DiscoveryManagerConfig = qrdbsdk.DiscoveryManagerConfig

// Runtime discovery constructors.
var (
	// NewDiscoveryManager builds a discovery Manager from its config.
	NewDiscoveryManager = qrdbsdk.NewDiscoveryManager

	// NewStaticDiscovery builds a static discovery method.
	NewStaticDiscovery = qrdbsdk.NewStaticDiscovery

	// NewMulticastDiscovery builds a multicast discovery method.
	NewMulticastDiscovery = qrdbsdk.NewMulticastDiscovery

	// NewDNSDiscovery builds a DNS SRV discovery method.
	NewDNSDiscovery = qrdbsdk.NewDNSDiscovery

	// NewTokenDiscovery builds a token-server discovery method.
	NewTokenDiscovery = qrdbsdk.NewTokenDiscovery
)

// ---------------------------------------------------------------------------
// Cluster-formation entry points (from go-qrdb/sdk/go types_discovery.go)
// ---------------------------------------------------------------------------

// DiscoveryOptions configures cluster-formation discovery (BuildDiscovery).
type DiscoveryOptions = qrdbsdk.DiscoveryOptions

// Cluster-formation constructors.
var (
	// BuildDiscovery constructs a DiscoveryBackend from DiscoveryOptions.
	BuildDiscovery = qrdbsdk.BuildDiscovery

	// BootstrapShard forms or joins a Raft shard using peer discovery.
	BootstrapShard = qrdbsdk.BootstrapShard
)
