### Fixed

- A first boot that re-enters for an existing admin who is suspended, locked or disabled now fails with a message to reactivate the account, instead of issuing a credential and a super-admin grant nobody can log in with (#1651).
- Nodes sharing a KMS backend no longer race on a tenant key's go-xkms name: reservation, creation, rollback and deletion hold a lease in the replicated store, so a failed create on one node cannot drop the record another node's key relies on (#1651).
