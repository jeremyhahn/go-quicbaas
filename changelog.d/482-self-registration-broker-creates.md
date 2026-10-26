### Security

- Self-service registration now creates the account in the broker, and the WebAuthn ceremony only binds a credential to it. Begin creates the tenant and a pending account with a derived handle, then opens the ceremony through the invite path. Finish binds the credential, grants the roles and activates the account. go-xkms `AllowPublicRegistration` is now always `false`, so a ceremony can no longer create an account with the email as its username. A failed or abandoned registration now removes its tenant and account instead of leaving an orphan tenant behind (#482).
