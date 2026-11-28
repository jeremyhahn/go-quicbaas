### Security

- Logging out everywhere over gRPC now revokes the caller's access and refresh credentials in its tenant, and refuses when it cannot, instead of deleting only session records (#1657).
