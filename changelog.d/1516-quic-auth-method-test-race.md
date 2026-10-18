### Fixed

- **The HTTP/3 auth-method tests no longer race their fake server** (#1516): the handler and the tests read and write its state only under one lock.
