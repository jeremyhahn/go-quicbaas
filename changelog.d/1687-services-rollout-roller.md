### Added

- The rollout controller drives a service's A/B slots: `rollout.PlaneRoller` fills the inactive slot, appraises it and switches the service's name to it, and a node whose service plane runtime supplies an `Appraiser` runs the one leader-gated controller, so `Plane.Redeploy` rolls a signed `Release` out (#1687).
