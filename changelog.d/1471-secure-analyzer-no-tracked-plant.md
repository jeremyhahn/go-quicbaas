### Fixed

- **`make secure-analyzer-self-test` never writes a tracked file.** The
  hardware-unit proof used to plant a memguard import into
  `cmd/quicbaas/piv_local_pkcs11.go` and restore it from a trap, so a SIGKILL
  left the plant in the working tree. Both planted sources now live in a temp
  dir outside the clone and reach `go vet` through `GOFLAGS=-overlay`; a run
  killed at any point leaves `git status` clean (#1471).
