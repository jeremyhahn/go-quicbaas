### Security

- The certificate `Chain` call, which imports the issuer certificates its AIA walk fetches, takes `trust:write` instead of `certs:generate` on gRPC, REST (`POST /api/v1/certificate/chain`) and MCP. On MCP, `quicbaas_trust_auto_import_aia` and `quicbaas_trust_publish_dane` take `trust:write` too, as they already did on gRPC and REST. A caller allowed only to issue certificates can no longer write the trust store (#2070).
