### Fixed

- **Deleting a role now strips it from every holder in the tenant
  (go-quicbaas#435).** The cascade used to read only the first 1000 users
  listed, so later holders kept the deleted role id. It now pages through
  every user. Users deleted while it runs no longer make it skip a holder:
  each page overlaps the one before, and if that shared user has moved,
  the cascade starts its scan again. A failure partway leaves the role in
  place, and a retry finishes the cascade.
- **The qrdb user and role adapters return exactly the offset a caller asks
  for.** An offset that was not a multiple of the limit used to return the
  whole page holding it, which made a role delete over a qrdb tenant with
  256 or more users loop until it was cancelled.
