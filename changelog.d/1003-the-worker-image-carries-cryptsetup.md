### Fixed

- The Software Factory worker image carries `cryptsetup`, and
  `make test-worker-image` asserts it. go-luks2's unit tier runs LUKS2
  conformance tests against the reference implementation and fails them by
  design when the binary is absent, so every go-luks2 delivery failed
  verification regardless of the change (#1003).
