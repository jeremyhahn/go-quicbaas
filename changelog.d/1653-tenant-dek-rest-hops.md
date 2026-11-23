### Security

- **Tenant DEK calls over REST no longer send key-protected data or the bearer in plaintext (#1653).** `Security().TenantDEK()` over REST refuses to send encrypt, decrypt and rewrap to anything but the configured https origin and follows no redirect for them; status, rotate and retire never send the platform bearer over http or across hosts.
