### Changed

- **`UserClient.Suspend` and `UserClient.Unsuspend` return
  `UserSuspensionUnsupportedError` (go-quicbaas#1552).** No transport served
  either verb. User suspension is go-qrdb IAM state, so both verbs now
  validate their input and return the typed error without calling the
  transport until the go-qrdb suspension seam (go-qrdb#801) lands.
