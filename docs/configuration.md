# Locktivity Remote Configuration

## epack.yaml Setup

Add a Locktivity remote:

```yaml
remotes:
  locktivity:
    adapter: locktivity
    source: locktivity/epack-remote-locktivity@v1
    secrets:
      - LOCKTIVITY_CLIENT_ID
      - LOCKTIVITY_CLIENT_SECRET
```

The `secrets` list is required so `epack` will pass these environment variables
through to the remote adapter process.

For brokered or managed-runner setups, `epack` may instead inject
`LOCKTIVITY_ACCESS_TOKEN` on the adapter's behalf. The adapter will always prefer
that short-lived access token over any local OAuth flow.

Custom endpoints belong in config and require explicit acknowledgement:

```yaml
remotes:
  locktivity-dev:
    adapter: locktivity
    source: locktivity/epack-remote-locktivity@v1
    insecure_endpoint: https://dev-tunnel.ngrok-free.app
    auth:
      insecure_endpoint: https://dev-tunnel.ngrok-free.app
```

You can define multiple remotes for different environments:

```yaml
remotes:
  locktivity-prod:
    adapter: locktivity
    source: locktivity/epack-remote-locktivity@v1
    secrets:
      - LOCKTIVITY_CLIENT_ID
      - LOCKTIVITY_CLIENT_SECRET
    target:
      environment: production

  locktivity-staging:
    adapter: locktivity
    source: locktivity/epack-remote-locktivity@v1
    secrets:
      - LOCKTIVITY_CLIENT_ID
      - LOCKTIVITY_CLIENT_SECRET
    target:
      environment: staging
```

## Authentication Configuration

### Manual Client Credentials

```bash
export LOCKTIVITY_CLIENT_ID="your-client-id"
export LOCKTIVITY_CLIENT_SECRET="your-client-secret"
epack push locktivity packs/evidence.epack
```

### Brokered Access Token

```bash
export LOCKTIVITY_ACCESS_TOKEN="your-short-lived-token"
epack push locktivity packs/evidence.epack
```

### Interactive Device Code Login

Set `LOCKTIVITY_AUTH_MODE=all` to enable interactive login and stored-token
refresh flows.

## Runtime Override Environment Variables

The preferred path is `epack.yaml`, but the adapter also accepts explicit runtime overrides:

- `EPACK_REMOTE_ENDPOINT` – trusted API endpoint passed by `epack` from `insecure_endpoint` config
- `EPACK_REMOTE_AUTH_ENDPOINT` – trusted auth endpoint passed by `epack` from `auth.insecure_endpoint` config

For standalone/manual use, `LOCKTIVITY_ENDPOINT` and `LOCKTIVITY_AUTH_ENDPOINT` are accepted
as backward-compatible aliases. All custom endpoints must use HTTPS.

## Troubleshooting

### "authentication required"

Set one of the supported auth inputs:
- `LOCKTIVITY_ACCESS_TOKEN`
- `LOCKTIVITY_CLIENT_ID`
- `LOCKTIVITY_CLIENT_SECRET`

For client credentials, set both:
- `LOCKTIVITY_CLIENT_ID`
- `LOCKTIVITY_CLIENT_SECRET`

### Pull by version returns not found

Ensure the specified version exists in the target environment and that your credentials include release read access.
