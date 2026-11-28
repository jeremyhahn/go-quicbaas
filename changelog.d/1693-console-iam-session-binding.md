### Fixed

- The console's IAM page shows a tenant-provisioning result, the admin's one-time invite token or certificate, only to the session that asked for it: a change of signed-in session closes the provisioning form and the result dialog, and a result or error that lands after the session changed is dropped, so a later session never sees the earlier one's secrets (#1693).
- The console's IAM page no longer offers "Revoke all sessions" for a user: the server deletes only the session records and leaves the login's access and refresh tokens valid, so the action is withheld until revocation ends login tokens. The user's session list stays, read-only (#1693).
