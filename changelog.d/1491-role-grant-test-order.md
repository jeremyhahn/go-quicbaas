### Fixed

- **`TestNewModule_RegistersRoleGrants` passes in any test order** (#1491).
  The module's Admin/Owner gap fill is computed by the pure `roleGrantGaps`
  and proved against qrdb's own placements, so a go-xkms module built earlier
  in the process no longer fails it. A new test composes both modules as the
  server does and pins the owner ruling of 2026-10-17: Admin holds `keys:sign`,
  Operator still holds it, and the Security Officer does not. No grant changed.
