### Added

- The service plane's deploy grants are built by the node: a node started with `server.WithServicePlane` and no grants gives each deployed service its go-xkms workload identity, a TLS certificate for `<name>.<tenant>.<domain>`, a registered name, its built-in role and its secrets namespace, and revokes all five on undeploy (#1687).
