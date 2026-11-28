### Fixed

- The TypeScript SDK's REST client sends a policy document's `spec_yaml` as the base64 of its UTF-8 text when it creates, updates or validates a policy, which is what the server reads. Before, a valid document was refused with HTTP 400 "invalid JSON body". The request types still take the document text (#1693).

### Removed

- The TypeScript SDK's `entities.create` is withheld until the server assigns an id on POST: each create wrote the entity under id 0 and replaced the one before it. Use `entities.put(type, id, data)` (#1693).
