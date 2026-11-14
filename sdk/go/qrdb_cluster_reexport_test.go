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

import (
	"context"
	"errors"
	"testing"

	qrdbcluster "github.com/jeremyhahn/go-qrdb/pkg/cluster"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	qrdbembedded "github.com/jeremyhahn/go-qrdb/sdk/go/transport/embedded"

	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// The aliases are identities: a value of the go-qrdb type is assignable to
// the QuicBaaS name and back, with no conversion.
var (
	_ *qrdbsdk.ClusterClient              = (*QRDBClusterClient)(nil)
	_ qrdbsdk.Member                      = QRDBClusterMember{}
	_ qrdbsdk.MembershipEvent             = QRDBMembershipEvent{}
	_ qrdbsdk.APIProtocol                 = QRDBAPIProtocol(qrdbsdk.APIProtocolREST)
	_ qrdbcluster.Member                  = QRDBRaftMember{}
	_ *qrdbcluster.MembershipManager      = (*QRDBMembershipManager)(nil)
	_ qrdbcluster.MembershipConfig        = QRDBMembershipConfig{}
	_ qrdbcluster.MembershipOption        = QRDBMembershipOption(nil)
	_ qrdbcluster.MembershipStore         = QRDBMembershipStore(nil)
	_ qrdbcluster.Reconfigurer            = QRDBMembershipReconfigurer(nil)
	_ *qrdbcluster.Advertiser             = (*QRDBNodeAdvertiser)(nil)
	_ qrdbcluster.AdvertiseConfig         = QRDBAdvertiseConfig{}
	_ qrdbcluster.AdvertiseOption         = QRDBAdvertiseOption(nil)
	_ qrdbtransport.RebalanceClient       = QRDBRebalanceClient(nil)
	_ *qrdbtransport.ShardStatsResponse   = (*QRDBShardStatsResponse)(nil)
	_ *qrdbtransport.RebalancePlanRequest = (*QRDBRebalancePlanRequest)(nil)
	_ *qrdbtransport.RebalancePlan        = (*QRDBRebalancePlan)(nil)
	_ *qrdbtransport.RebalanceResult      = (*QRDBRebalanceResult)(nil)
	_ *qrdbtransport.ImbalanceReport      = (*QRDBImbalanceReport)(nil)
	_ QRDBMembershipClient                = (*qrdbsdk.Client)(nil)
	_ Client                              = (*client)(nil)
)

type addReplicaCall struct {
	shardID, replicaID uint64
	raftAddr, identity string
}

// recordingAdminService answers the Admin calls ClusterClient is a facade
// over, so a test proves the re-export reaches go-qrdb's AdminServicer.
type recordingAdminService struct {
	stubAdminService
	added []addReplicaCall
}

func (r *recordingAdminService) AddMember(_ context.Context, shardID, replicaID uint64, raftAddr, identity string) error {
	r.added = append(r.added, addReplicaCall{shardID, replicaID, raftAddr, identity})
	return nil
}

func (r *recordingAdminService) Peers(context.Context) (*qrdbsdk.PeersInfo, error) {
	return &qrdbsdk.PeersInfo{Shards: []qrdbsdk.ShardPeers{{
		ShardID:  7,
		LeaderID: 2,
		Peers: []qrdbsdk.PeerInfo{
			{ReplicaID: 1, Address: "raft-1:7000"},
			{ReplicaID: 2, Address: "raft-2:7000", IsLeader: true},
		},
	}}}, nil
}

func (r *recordingAdminService) GetShardState(_ context.Context, shardID uint64) (*qrdbsdk.ShardStateResponse, error) {
	return &qrdbsdk.ShardStateResponse{ShardID: shardID, LeaderID: 2, HasLeader: true}, nil
}

func TestQRDBCluster_ReachesEmbeddedAdminServicer(t *testing.T) {
	admin := &recordingAdminService{}
	c := buildEmbeddedQRDBClient(t, QRDBEmbeddedServicers{KV: stubKVService{}, Admin: admin})
	ctx := context.Background()

	cc, err := c.QRDBCluster()
	if err != nil {
		t.Fatalf("QRDBCluster: %v", err)
	}
	if cc == nil {
		t.Fatal("QRDBCluster returned a nil client without an error")
	}

	if err := cc.AddReplica(ctx, 7, 3, "raft-3:7000", "node-3"); err != nil {
		t.Fatalf("AddReplica: %v", err)
	}
	want := addReplicaCall{7, 3, "raft-3:7000", "node-3"}
	if len(admin.added) != 1 || admin.added[0] != want {
		t.Fatalf("AddMember calls = %+v, want [%+v]", admin.added, want)
	}

	members, err := cc.Members(ctx, 7)
	if err != nil {
		t.Fatalf("Members: %v", err)
	}
	if len(members) != 2 {
		t.Fatalf("Members = %+v, want 2 replicas", members)
	}
	var leader QRDBClusterMember
	for _, m := range members {
		if m.IsLeader {
			leader = m
		}
	}
	if leader.ReplicaID != 2 || leader.Address != "raft-2:7000" {
		t.Errorf("leader member = %+v, want replica 2 at raft-2:7000", leader)
	}

	id, ok, err := cc.Leader(ctx, 7)
	if err != nil || !ok || id != 2 {
		t.Errorf("Leader = (%d, %v, %v), want (2, true, nil)", id, ok, err)
	}
}

func TestQRDBMembership_IsTheQRDBClient(t *testing.T) {
	c := buildEmbeddedQRDBClient(t, QRDBEmbeddedServicers{KV: stubKVService{}, Admin: stubAdminService{}})

	mc, err := c.QRDBMembership()
	if err != nil {
		t.Fatalf("QRDBMembership: %v", err)
	}
	qc, err := c.qrdbClient()
	if err != nil {
		t.Fatalf("qrdbClient: %v", err)
	}
	if got, ok := mc.(*qrdbsdk.Client); !ok || got != qc {
		t.Fatalf("QRDBMembership = %T %p, want the lazily built qrdb client %p", mc, mc, qc)
	}
	if mc.NewMembershipReconfigurer() == nil {
		t.Error("NewMembershipReconfigurer returned nil")
	}
	if mc.NewMembershipStore() == nil {
		t.Error("NewMembershipStore returned nil")
	}
}

// noRebalanceTransport carries only the base qrdb Transport method set, so
// go-qrdb's Client.Rebalance finds no RebalanceClient and returns nil.
type noRebalanceTransport struct {
	qrdbtransport.Transport
}

func TestQRDBRebalance_NilFromTransportIsATypedError(t *testing.T) {
	base, err := qrdbembedded.New(stubKVService{}, stubAdminService{})
	if err != nil {
		t.Fatalf("qrdbembedded.New: %v", err)
	}
	qc, err := qrdbsdk.NewClient(noRebalanceTransport{base})
	if err != nil {
		t.Fatalf("qrdbsdk.NewClient: %v", err)
	}
	if qc.Rebalance() != nil {
		t.Fatal("precondition: the wrapped transport must not expose rebalance")
	}
	c := &client{opts: defaultOptions(), address: "embedded://local"}
	c.qrdb.once.Do(func() { c.qrdb.client = qc })

	rc, err := c.QRDBRebalance()
	if rc != nil {
		t.Fatalf("QRDBRebalance returned client %v, want nil", rc)
	}
	var unsupported *sdkerrors.QRDBRebalanceUnsupportedError
	if !errors.As(err, &unsupported) {
		t.Fatalf("QRDBRebalance err = %T %v, want *QRDBRebalanceUnsupportedError", err, err)
	}
	if !sdkerrors.IsUnimplemented(err) {
		t.Errorf("QRDBRebalance err = %v, want CodeUnimplemented", err)
	}
}

func TestQRDBClusterAccessors_EmbeddedWithoutServicersFailFast(t *testing.T) {
	newClient := func() *client {
		c := &client{opts: defaultOptions(), address: "embedded://local"}
		c.opts.transportType = TransportEmbedded
		return c
	}
	checks := map[string]func(*client) (any, error){
		"QRDBCluster":    func(c *client) (any, error) { return c.QRDBCluster() },
		"QRDBMembership": func(c *client) (any, error) { return c.QRDBMembership() },
		"QRDBRebalance":  func(c *client) (any, error) { return c.QRDBRebalance() },
	}
	for name, call := range checks {
		t.Run(name, func(t *testing.T) {
			_, err := call(newClient())
			if !sdkerrors.IsInvalidInput(err) {
				t.Fatalf("%s err = %T %v, want InvalidInput", name, err, err)
			}
		})
	}
}
