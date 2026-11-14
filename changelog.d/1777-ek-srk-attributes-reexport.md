### Added

- **sdk/go/xkms re-exports EKAttributesFromConfig and SRKAttributesFromConfig
  (go-quicbaas#1777).** A consumer builds its EK and SRK creation attributes
  from its TPM config through the quicbaas SDK, as it already does for IDevID,
  LDevID and keys.
