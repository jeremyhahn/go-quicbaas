### Fixed

- **Deleting a role now strips it from every holder in the tenant
  (go-quicbaas#435).** The cascade used to read only the first 1000 users
  listed, so later holders kept the deleted role id. It now pages through
  every user. A failure partway leaves the role in place, and a retry
  finishes the cascade.
