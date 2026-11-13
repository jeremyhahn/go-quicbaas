### Added

- **sdk/go/xkms re-exports the signer store (#1816).**
  `SignerStorer` and `NewSignerStore` let a consumer's keystore implement a
  go-xkms Backend through the quicbaas SDK.
