### Security

- The worker image's gosec links golang.org/x/net v0.62.0 (CVE-2026-78661): the `golang.org/x/text` floor in `GO_TOOL_DEPENDENCY_FLOORS` moves to v0.43.0, which x/net v0.62.0 requires, so applying it no longer downgrades x/net to v0.60.0 (#2635).
- The worker image and `make install-gosec` apply the Go tool dependency floors in one `go get`, so a floor list that disagrees with itself fails the build instead of shipping a lowered floor (#2635).
