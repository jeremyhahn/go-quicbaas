### Security

- The REST and MCP federation-provider create, get, list and update responses no longer return the upstream OAuth2 client secret; `client_secret` is now always empty in a provider response. The secret is still accepted on create and update, stored, and used for the IdP token exchange (#1626).
