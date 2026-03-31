# epack-remote-locktivity

Remote adapter for [epack](https://github.com/locktivity/epack) that enables `push`, `pull`, and `runs sync` operations with the Locktivity registry.

See [docs/](docs/) for detailed documentation:
- [docs/overview.md](docs/overview.md)
- [docs/configuration.md](docs/configuration.md)
- [docs/examples.md](docs/examples.md)

## Features

- **Push**: Upload evidence packs and create releases
- **Pull**: Download packs by latest release or release ID
- **Run Sync**: Sync collector run ledgers for audit trails
- **Auth Modes**: Brokered access token, client credentials, and optional device code login
- **Secure Storage**: OS keychain integration for interactive login tokens
- **Hardened Finalize Tokens**: Signed, expiring, single-use finalize tokens
- **Protocol Enforcement**: Requires `protocol_version: 1` on all requests
- **Rate Limits**: Built-in throttling for `auth.login` and `pull.prepare`

## Installation

```bash
go install github.com/locktivity/epack-remote-locktivity/cmd/epack-remote-locktivity@latest
```

Or build from source:

```bash
git clone https://github.com/locktivity/epack-remote-locktivity
cd epack-remote-locktivity
make build
```

## Quick Start

```yaml
# epack.yaml
remotes:
  locktivity:
    adapter: locktivity
    source: locktivity/epack-remote-locktivity@v1
    target:
      workspace: myorg
      environment: prod
```

```bash
# Managed runners typically inject a short-lived access token
export LOCKTIVITY_ACCESS_TOKEN="..."

# Push local pack
epack push locktivity packs/evidence.epack

# Pull latest release
epack pull locktivity
```

Pull by release ID is also supported:

```bash
epack pull locktivity --release rel_abc123
```

Pull by version is also supported:

```bash
epack pull locktivity --version v1.0.0
```

Pull by digest is also supported:

```bash
epack pull locktivity --digest sha256:abc123...
```

## Authentication

The adapter prefers a pre-resolved `LOCKTIVITY_ACCESS_TOKEN` when present. That is
the expected path for brokered or managed-runner setups:

```bash
export LOCKTIVITY_ACCESS_TOKEN="..."
epack push locktivity packs/evidence.epack
```

For manual setups, client credentials are still supported:

```bash
export LOCKTIVITY_CLIENT_ID="..."
export LOCKTIVITY_CLIENT_SECRET="..."
epack push locktivity packs/evidence.epack
```

For local interactive use, device code login is available when
`LOCKTIVITY_AUTH_MODE=all`.

## Supported Operations

The binary implements Remote Adapter Protocol v1 operations:

| Operation | Description |
|-----------|-------------|
| `push.prepare` | Request upload URL and create pack record |
| `push.finalize` | Finalize upload and create release |
| `pull.prepare` | Resolve latest release or a release ID |
| `pull.finalize` | Confirm download completion |
| `runs.sync` | Sync run ledgers to Locktivity |
| `auth.login` | Start device code flow |
| `auth.whoami` | Return current identity status |

All requests must include `protocol_version: 1`.

## Runtime Endpoint Overrides

The release binary always defaults to:

- API: `https://api.locktivity.com`
- Auth: `https://app.locktivity.com`

For enterprise, staging, or local-development environments, declare custom endpoints in `epack.yaml`:

```yaml
remotes:
  locktivity:
    source: locktivity/epack-remote-locktivity@v1
    insecure_endpoint: https://dev-tunnel.ngrok-free.app
    auth:
      insecure_endpoint: https://dev-tunnel.ngrok-free.app
```

`epack` passes those values to the adapter using trusted explicit env. The adapter also supports
direct environment-based overrides for standalone/manual testing:

```bash
EPACK_REMOTE_ENDPOINT=https://dev-tunnel.ngrok-free.app \
EPACK_REMOTE_AUTH_ENDPOINT=https://dev-tunnel.ngrok-free.app \
./bin/epack-remote-locktivity --capabilities
```

## Environment Variables

| Variable | Description |
|----------|-------------|
| `LOCKTIVITY_ACCESS_TOKEN` | Pre-resolved bearer token for brokered or managed-runner auth |
| `LOCKTIVITY_CLIENT_ID` | OAuth client ID for client credentials |
| `LOCKTIVITY_CLIENT_SECRET` | OAuth client secret for client credentials |
| `LOCKTIVITY_AUTH_MODE` | Auth mode: `client_credentials_only` (default) or `all` (enables device code login and stored-token refresh) |
| `EPACK_REMOTE_ENDPOINT` | Trusted API endpoint override passed by `epack` (from `insecure_endpoint` config) |
| `EPACK_REMOTE_AUTH_ENDPOINT` | Trusted auth endpoint override passed by `epack` (from `auth.insecure_endpoint` config) |
| `LOCKTIVITY_ENDPOINT` | Backward-compatible API endpoint override for standalone/manual use |
| `LOCKTIVITY_AUTH_ENDPOINT` | Backward-compatible auth endpoint override for standalone/manual use |

## Security Notes

- Finalize tokens (`push.finalize`, `pull.finalize`) are HMAC-signed and include:
  - Expiration (`exp`)
  - Nonce (`nonce`) with replay protection (single-use)
- `pull.finalize` verifies request digest matches token digest
- Custom endpoints must use HTTPS, are blocked by `EPACK_STRICT_PRODUCTION=true`,
  and emit an insecure-bypass audit event
- Keychain entries are namespaced by auth endpoint host to avoid cross-environment token collisions
- Stored keychain tokens include expiry metadata and are refreshed using refresh tokens when expired
- `auth.login` and `pull.prepare` are rate-limited to reduce abuse potential

## Development

### Prerequisites

- Go 1.26+

### Commands

```bash
# Release build
make build

# Unit tests
make test

# Lint
make lint

# SDK conformance tests
make sdk-test

# Run through the SDK harness
make sdk-run
```

`make sdk-test` runs `epack-conformance` from your Go bin directory directly.
`make sdk-run` requires an `epack` binary with SDK commands available in your `PATH`.
Direct `epack sdk test ...` invocations also require `epack-conformance` on `PATH`, typically via
`export PATH="$PATH:$(go env GOPATH)/bin"` or a configured `GOBIN`.

## License

Apache-2.0
