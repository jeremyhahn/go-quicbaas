### Fixed

- The embedded transport now routes the five `quicbaas.IAM.Audit` verbs (Query, Get, Record, RecordAsync, Delete) to the audit logger. Each verb applies the tenant rule, and Delete requires an authenticated subject that holds `audit:delete` (#1555).
