### Fixed

- **A self-registered tenant creator holds the tenant's Admin role, or no
  account at all (go-quicbaas#358).** Registration wrote the role names into
  `User.Roles`, which holds role IDs, and discarded the write's error, so the
  creator got a session without the Admin role. It now resolves the built-in
  `admin` and default roles to their IDs and assigns them. If a grant fails,
  finish returns an error and mints no session, and the account is deleted.
