### Added

- **sdk/go/xkms re-exports the signer store (go-quicbaas#1812).**
  `SignerStorer` and `NewSignerStore` let a consumer's keystore implement a
  go-xkms Backend through the quicbaas SDK.
