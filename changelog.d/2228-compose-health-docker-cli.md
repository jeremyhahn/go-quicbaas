### Fixed

- Ten compose healthchecks probed `/v1/health/live`, which the REST router does not serve, so those containers never reported healthy; they now call `/api/v1/health/live`, and `health_paths_test.go` checks every compose healthcheck path against the REST route table (#2228).
- The runtime image now carries the docker CLI and git, so a containerised daemon with the host's docker socket mounted can start worker and train containers and run the SDLC pipeline (#2228).
