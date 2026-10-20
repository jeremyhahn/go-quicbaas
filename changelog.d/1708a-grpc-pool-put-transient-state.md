### Fixed

- The gRPC connection `Pool.Put` now frees a returned connection whatever its transient state: only a connection that is shut down or older than `MaxAge` is closed and replaced. A connection returned while passing through Connecting or TransientFailure stays in the pool, `Get` skips it until it is Ready or Idle, and the health-check loop replaces it if it stays down; before, such a connection was closed and replaced and stayed marked in use, so a cleanly ended object-transfer stream could leave its pooled connection unreturned (#1709).
