### Changed

- **The legacy `/api/v1/seal` endpoints send `Deprecation` as an RFC 9745
  Structured Field Date.** The value changes from the HTTP-date
  `Sun, 01 May 2026 00:00:00 GMT` (an early draft's format) to
  `@1777593600`, the same instant as RFC 9651 Date. A client that parsed the
  old HTTP-date must parse the new form. `Sunset` stays an RFC 8594
  HTTP-date and `Link` still names `/api/v1/barrier` as `successor-version`.
