### Security

- **App and MFA verbs honour a request's tenant id only for the caller's own
  tenant (go-quicbaas#1312).** The service layer's app register and list, the
  embedded transport's app register, list, get and delete, and the MCP and
  gRPC MFA verbs that take a tenant id (begin enrollment, status, disable,
  begin challenge, backup codes, and gRPC verify code) used the request's
  tenant id unchecked. A caller could therefore register or list apps in, or
  enroll, read, disable or mint backup codes for a user of, another tenant.
  These verbs now refuse a tenant id that is not the caller's own; the
  system tenant gets no lift. The embedded app get and delete also look the
  client up inside the caller's tenant.
