# Local Learnings & Project Memory

## Preferences
- Use minimal, dependency-light Go implementations with standard library first (ponytail principle).
- Use pure-Go SQLite (`modernc.org/sqlite`) to avoid CGO cross-compilation headaches.
- Use Chi router for net/http compatibility and lightweight middleware.

## Workflows
- Always run unit tests (`go test ./...`) after major module implementations.
- Proactively handle graceful degradation when external services (like Redis) are absent in dev environments.

## Constraints
- URLs must be canonicalized deterministically (lowercase host, strip tracking parameters, sort query parameters) before hashing to prevent duplicate IDs.
- Validate SSRF on all upstream requests (disallow loopback, private IPv4/IPv6, link-local).
