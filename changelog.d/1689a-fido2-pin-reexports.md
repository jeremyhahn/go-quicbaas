### Added

- **sdk/go/xkms/fido2 re-exports the FIDO2 PIN errors and assertion types
  (go-quicbaas#1689).** A caller unlocking with a PIN-protected FIDO2 key tells
  a missing, wrong or blocked PIN apart, and reads the attempts left, through
  the quicbaas SDK.
