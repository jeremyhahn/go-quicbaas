### Added

- The leader takes over service rollouts in flight: `Controller.ResumeAll` resumes every rollout that is not finished, and the node's service plane recovery tick calls it, so a rollout left behind by a leader that stepped down or a node that restarted completes or rolls back instead of holding its service busy (#1687).
