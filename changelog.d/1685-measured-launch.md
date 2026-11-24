### Security

- Measured container launch (#1685): an attested or measured launch now
  starts only after a verified host appraisal; the Verifier rebuilds each
  measurement log entry from its recorded image, configuration and policy
  digests; an attestation reports the measurement the node logged at launch;
  the stored attestation result includes the launch-log verdict; DSSE
  provenance and cluster manifests are verified with the algorithm they were
  signed with; and cluster manifest updates are compare-and-swap, so
  concurrent writers cannot roll a manifest back.
