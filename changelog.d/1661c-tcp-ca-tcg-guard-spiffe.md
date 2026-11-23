### Security

- Over the gRPC (TCP) CA service, TCG certificates (EK, AK, IDevID, device enrollment) are issued for the system tenant only, as on every other transport, and certificates issued for a tenant carry the configured SPIFFE trust domain's tenant SAN (#1661).
