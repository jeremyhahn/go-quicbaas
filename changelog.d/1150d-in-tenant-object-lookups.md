### Security

- **App and MFA objects are looked up inside the caller's tenant
  (go-quicbaas#1312).** The handlers checked that a request named the
  caller's own tenant, then fetched the object by its id alone. This
  affected app get, update, delete and secret rotation, and MFA enrollment
  complete and cancel and challenge verify. A caller could therefore reach
  another tenant's app or MFA record by its id. These verbs now look the
  object up inside the resolved tenant on REST, gRPC, MCP (MFA) and the
  service layer (apps). REST also binds an MFA record to the URL's user.
  Another tenant's object answers the same not-found as a missing one.
- **Cancelling an unknown MFA enrollment id now answers not-found** instead
  of succeeding.
