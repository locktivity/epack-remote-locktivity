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
