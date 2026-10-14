### Security

- **An authenticated caller with an empty tenant is not the system tenant
  (go-quicbaas#348).** Only a principal bound to the system tenant id itself
  (`auth.IsSystemSubject`) acts as system. The per-tenant barrier gates now
  refuse an authenticated empty-tenant caller with `ErrForbidden`, so it can
  no longer read every tenant's barrier status. The embedded transport's IAM
  admin and account-reservation verbs refuse a bound empty-tenant subject with
  `PermissionDeniedError` instead of letting it keep whatever tenant it named.
  An unbound in-process context (the embedding host, no subject) keeps its
  access.
