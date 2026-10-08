### Fixed

- **The Software Factory worker image carries the C headers portfolio cgo
  dependencies include (go-quicbaas#1164).** `libssl-dev` (go-tpm-tools' TPM
  simulator, `openssl/aes.h`) and `libusb-1.0-0-dev` (go-xkms `xkey`) join
  `build-essential`, so go-xkms `make lint` typechecks in a lane and in
  `lane-verify`. A new image probe compiles and runs a cgo program including
  both headers.
