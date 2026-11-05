### Changed

- Access tokens minted from `Claims` no longer carry `acr` or `amr`: no consumer read them for step-up, so they are ID-token claims only. Read them from the ID token (#392).
