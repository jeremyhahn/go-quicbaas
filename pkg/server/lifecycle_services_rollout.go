package server

import (
	"context"
	"errors"
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

// servicePlaneResumer is the Roller the node's recovery asks to take over
// rollouts in flight; rollout.PlaneRoller is one.
type servicePlaneResumer interface {
	ResumeAll(ctx context.Context) ([]*rollout.Slots, error)
}

// resumeServicePlaneRollouts takes over the rollouts in flight when the
// plane has the node's rollout controller and this node leads. A plane
// with no Roller, or a follower, does nothing.
func (s *Server) resumeServicePlaneRollouts(ctx context.Context, plane *serviceplane.Plane) {
	if plane == nil {
		return
	}
	resumer, ok := plane.Roller().(servicePlaneResumer)
	if !ok {
		return
	}
	resumed, err := resumer.ResumeAll(ctx)
	if err != nil && !errors.Is(err, rollout.ErrNotLeader) {
		s.logger.WithError(err).Warn("service plane: rollouts in flight not all resumed")
	}
	for _, slots := range resumed {
		if slots == nil {
			continue
		}
		phase := rollout.Phase("")
		if slots.Rollout != nil {
			phase = slots.Rollout.Phase
		}
		s.logger.Infof("service plane: resumed the rollout of %s/%s: %s", slots.Tenant, slots.Service, phase)
	}
}

// servicePlaneRollerID names the node's controller in the rollouts it
// claims: its raft node ID in decimal, or "node" when it has none.
func (s *Server) servicePlaneRollerID() string {
	if s.config == nil || s.config.Raft.NodeID == 0 {
		return "node"
	}
	return strconv.FormatUint(s.config.Raft.NodeID, 10)
}
