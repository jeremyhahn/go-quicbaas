### Fixed

- **The embedded transport routes Secrets Rotate, UpdateStatus,
  SetRotationPolicy and GetRotationPolicy** (#1540). The SDK secrets client
  sent all four, and the embedded transport answered each with
  `UnknownMethodError`. An unknown secret status is refused as invalid input
  before it reaches the service.
- **Embedded KMS UpdateKeyStatus accepts the request the SDK client sends**
  (#1541). `handleKMSUpdateKeyStatus` took only the service request type, so
  every `kms.Client.UpdateKeyStatus` call (disable, enable) failed with an
  invalid request type. It now converts the client request; an unknown status
  or a negative version is refused as invalid input.
