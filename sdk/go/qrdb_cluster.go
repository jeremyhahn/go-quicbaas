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

package quicbaas

// Re-exports of go-qrdb's cluster, membership and rebalance services (SPEC
// row `cluster`, #1649). go-qrdb owns cluster membership, leadership and
// shard rebalancing; QuicBaaS consumers reach them through these aliases and
// the Client accessors QRDBCluster, QRDBMembership and QRDBRebalance, over
// the same lazily built qrdb sub-client as SealStore and TenantMgmt. No
// QuicBaaS-side logic sits between the caller and go-qrdb.

import (
	qrdbcluster "github.com/jeremyhahn/go-qrdb/pkg/cluster"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// Cluster: membership and leadership over the qrdb Admin surface.

// QRDBClusterClient is a re-export of the qrdb ClusterClient: AddReplica,
// RemoveReplica, Members, Leader, LeaderEndpoint and WatchMembership.
type QRDBClusterClient = qrdbsdk.ClusterClient

// QRDBClusterMember is a re-export of the qrdb Member: one Raft replica of a
// shard as ClusterClient.Members reports it.
type QRDBClusterMember = qrdbsdk.Member

// QRDBMembershipEvent is a re-export of the qrdb MembershipEvent emitted by
// ClusterClient.WatchMembership.
type QRDBMembershipEvent = qrdbsdk.MembershipEvent

// QRDBAPIProtocol is a re-export of the qrdb APIProtocol: a client-facing API
// protocol a replica advertises an endpoint for.
type QRDBAPIProtocol = qrdbsdk.APIProtocol

// Membership: opt-in dynamic Raft membership and node advertisement.

// QRDBRaftMember is a re-export of the qrdb cluster Member: a member
// registration (Raft address, advertised API endpoints, features).
type QRDBRaftMember = qrdbcluster.Member

// QRDBMembershipManager is a re-export of the qrdb MembershipManager.
type QRDBMembershipManager = qrdbcluster.MembershipManager

// QRDBMembershipConfig is a re-export of the qrdb MembershipConfig.
type QRDBMembershipConfig = qrdbcluster.MembershipConfig

// QRDBMembershipOption is a re-export of the qrdb MembershipOption.
type QRDBMembershipOption = qrdbcluster.MembershipOption

// QRDBMembershipStore is a re-export of the qrdb MembershipStore interface.
type QRDBMembershipStore = qrdbcluster.MembershipStore

// QRDBMembershipReconfigurer is a re-export of the qrdb Reconfigurer
// interface.
type QRDBMembershipReconfigurer = qrdbcluster.Reconfigurer

// QRDBNodeAdvertiser is a re-export of the qrdb node Advertiser.
type QRDBNodeAdvertiser = qrdbcluster.Advertiser

// QRDBAdvertiseConfig is a re-export of the qrdb AdvertiseConfig.
type QRDBAdvertiseConfig = qrdbcluster.AdvertiseConfig

// QRDBAdvertiseOption is a re-export of the qrdb AdvertiseOption.
type QRDBAdvertiseOption = qrdbcluster.AdvertiseOption

// QRDBMembershipClient is the membership half of the qrdb client: the
// constructors that wire a membership manager, store, reconfigurer or node
// advertiser onto the client's lease, watch, KV and Admin surface.
// *QRDBClient satisfies it.
type QRDBMembershipClient interface {
	NewMembershipManager(self QRDBRaftMember, shardID uint64, cfg QRDBMembershipConfig, opts ...QRDBMembershipOption) (*QRDBMembershipManager, error)
	NewMembershipStore() QRDBMembershipStore
	NewMembershipStoreAt(prefix string) QRDBMembershipStore
	NewMembershipReconfigurer() QRDBMembershipReconfigurer
	NewNodeAdvertiser(self QRDBRaftMember, cfg QRDBAdvertiseConfig, opts ...QRDBAdvertiseOption) (*QRDBNodeAdvertiser, error)
}

var _ QRDBMembershipClient = (*QRDBClient)(nil)

// Rebalance: shard statistics and rebalance plans. QRDBRebalanceClient is
// declared with the other transport client aliases in qrdb.go.

// QRDBShardStatsResponse is a re-export of the qrdb ShardStatsResponse.
type QRDBShardStatsResponse = qrdbtransport.ShardStatsResponse

// QRDBRebalancePlanRequest is a re-export of the qrdb RebalancePlanRequest.
type QRDBRebalancePlanRequest = qrdbtransport.RebalancePlanRequest

// QRDBRebalancePlan is a re-export of the qrdb RebalancePlan.
type QRDBRebalancePlan = qrdbtransport.RebalancePlan

// QRDBRebalanceResult is a re-export of the qrdb RebalanceResult.
type QRDBRebalanceResult = qrdbtransport.RebalanceResult

// QRDBImbalanceReport is a re-export of the qrdb ImbalanceReport.
type QRDBImbalanceReport = qrdbtransport.ImbalanceReport

// QRDBCluster returns go-qrdb's ClusterClient for shard membership and
// leadership. The qrdb sub-client is lazily initialized on first call.
func (c *client) QRDBCluster() (*QRDBClusterClient, error) {
	qrdbClient, err := c.qrdbClient()
	if err != nil {
		return nil, err
	}
	return qrdbClient.Cluster(), nil
}

// QRDBMembership returns go-qrdb's membership constructors bound to the qrdb
// sub-client. The qrdb sub-client is lazily initialized on first call.
func (c *client) QRDBMembership() (QRDBMembershipClient, error) {
	qrdbClient, err := c.qrdbClient()
	if err != nil {
		return nil, err
	}
	return qrdbClient, nil
}

// QRDBRebalance returns go-qrdb's RebalanceClient. When the active qrdb
// transport does not implement the rebalance service it returns a
// *errors.QRDBRebalanceUnsupportedError, never a nil client.
func (c *client) QRDBRebalance() (QRDBRebalanceClient, error) {
	qrdbClient, err := c.qrdbClient()
	if err != nil {
		return nil, err
	}
	rc := qrdbClient.Rebalance()
	if rc == nil {
		return nil, sdkerrors.NewQRDBRebalanceUnsupported("QRDBRebalance")
	}
	return rc, nil
}
