package server

import (
	"strconv"

	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/serviceplane"
	"github.com/jeremyhahn/go-quicbaas/pkg/serviceplane/rollout"
)

// servicePlaneRoller builds the node's one rollout controller over plane:
// it claims slot records through guard, reads them from kv, steps only
// while leader answers that this node leads (nil is always leader),
// verifies releases against the runtime's keyring and drives the plane's
// own slot verbs, appraising each filled slot with the runtime's
// Appraiser.
//
// It answers nil, failing closed, when the runtime supplies no Appraiser,
// or a Verifier that is not a non-empty serviceplane.Keyring: the plane
// then has no Roller and a release redeploy answers ErrConfig.
func (s *Server) servicePlaneRoller(rt service.ServicePlaneRuntime, plane *serviceplane.Plane, guard serviceplane.Guard, kv serviceplane.Reader, leader scheduler.LeaderChecker) serviceplane.Roller {
	if rt.Appraiser == nil || plane == nil {
		return nil
	}
	keyring, ok := rt.Verifier.(serviceplane.Keyring)
	if !ok || len(keyring) == 0 {
		return nil
	}
	return rollout.PlaneRoller{Controller: &rollout.Controller{
		ID: s.servicePlaneRollerID(), Guard: guard, KV: kv, Leader: leader, Verifier: keyring,
		Strategy: rollout.NewPlaneStrategy(plane, rt.Appraiser),
	}}
}

// servicePlaneRollerID names the node's controller in the rollouts it
// claims: its raft node ID in decimal, or "node" when it has none.
func (s *Server) servicePlaneRollerID() string {
	if s.config == nil || s.config.Raft.NodeID == 0 {
		return "node"
	}
	return strconv.FormatUint(s.config.Raft.NodeID, 10)
}
