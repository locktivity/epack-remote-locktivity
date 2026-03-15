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

### Client Credentials (Current Supported Mode)

```bash
export LOCKTIVITY_CLIENT_ID="your-client-id"
export LOCKTIVITY_CLIENT_SECRET="your-client-secret"
epack push locktivity packs/evidence.epack
```

OIDC and interactive device-code login are both coming soon.

## Troubleshooting

### "authentication required"

Missing client credentials. Set both:
- `LOCKTIVITY_CLIENT_ID`
- `LOCKTIVITY_CLIENT_SECRET`

### Pull by version returns not found

Ensure the specified version exists in the target environment and that your credentials include release read access.
