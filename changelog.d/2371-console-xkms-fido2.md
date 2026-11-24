### Added

- The web console's Key Store page has a FIDO2 passkeys section, which adds a passkey to the signed-in account, registers a passkey for a user by email and display name, and tests a passkey by email through the xkms FIDO2 ceremonies, showing whether the server accepted it with the credential id or the username. A token in a finish answer is never shown; the result says only that one was issued. A cancelled browser ceremony, options the browser cannot use, a browser without passkey support and a connection without the FIDO2 client are shown inline in the section (#2371).
