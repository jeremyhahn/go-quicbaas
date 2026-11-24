### Fixed

- Dynamic client registration with an initial access token works on the unified server: `POST /oauth2/register` takes the tenant of the verified caller, so the SDK integration bootstrap registers its client again, and a caller without a token is still refused with 401. A dynamically registered client gets no permissions until an administrator grants them through the app-update route, and the integration bootstrap now does so before it requests a scoped token (#2707).
