### Documentation

- The `acr` / `amr` contract now names its one access-token exception: the mTLS service-account token from `/oauth2/token-mtls` carries `amr` (`["mtls"]`, or `["mtls","hwk"]` for a TPM-chained certificate) and no `acr`, since that grant issues no ID token. Every other access token carries neither (#1665).
