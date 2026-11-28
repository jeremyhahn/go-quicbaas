### Fixed

- A session renewal that fails partway, for example on a storage error, no
  longer logs the user out. Before, the presented refresh token was revoked
  before its replacement was issued, so the failure left the session with no
  refresh token that worked. Now the presented refresh token still renews the
  session, and a successful renewal still revokes it (#282).
