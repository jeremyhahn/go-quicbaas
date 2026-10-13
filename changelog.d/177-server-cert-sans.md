### Fixed

- **The auto-issued server TLS certificate honours configured SANs and no
  longer repeats them (go-quicbaas#177).**
  - Each SAN now appears once. A cert used to carry the host name up to four
    times and `127.0.0.1` twice. DNS names are compared without regard to
    case, and IPs by value.
  - A SAN added to `server.additional_san_dns` or
    `server.additional_san_ips` now takes effect at the next restart. The
    server used to keep serving the first certificate it issued. It now
    re-issues the certificate over the same key, revokes the old one as
    superseded, and logs the added SANs. A certificate a previous boot
    revoked but did not delete is replaced without a second revocation, so
    an interrupted re-issue no longer blocks startup.
  - The external-CA path now includes the additional SANs too.
  - The config validator rejects an entry in `server.additional_san_ips`
    that is not an IP address.
