### Changed

- The worker image's toolchain moves to its upstreams' latest releases: Claude Code 2.1.330, Codex 0.171.0, golangci-lint v2.15.0, gosec v2.30.0, govulncheck v1.10.0, gopls v0.24.0, serena v2.1.0, trivy v0.76.2, opencode v1.19.6, Node v26.13.1, uv 0.13.9, typescript-language-server 6.1.0, yaml-language-server 2.0.0, rust-analyzer 2026-11-23, and the Go tool dependency floors to x/mod v0.43.0, x/net v0.63.0, x/text v0.44.0, x/tools v0.53.0 and grpc v1.85.0; serena's dependency floors keep only filelock (#2708).
- The worker toolchain currency check skips Node pre-releases, taking the newest stable release, and checks trivy's pin against its GitHub releases (#2708).
