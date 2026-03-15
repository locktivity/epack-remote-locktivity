# Locktivity Remote Overview

The Locktivity remote adapter connects `epack` to the Locktivity registry for evidence pack distribution and run ledger synchronization.

## What It Does

| Capability | Description |
|------------|-------------|
| Push | Creates a pack record, uploads via presigned URL, and finalizes a release |
| Pull | Resolves and downloads the latest release, or a specific release by ID |
| Run Sync | Sends run ledger metadata tied to a pack digest |
| Whoami | Returns whether current credentials are authenticated |

## Request Flow

### Push

1. `push.prepare` creates or resolves a pack by file digest and returns upload info.
   - `stream` is extracted from the uploaded pack during ingestion
   - Stream provenance is validated as part of pack verification, not as an explicit API parameter
2. The pack is uploaded directly to storage.
3. `push.finalize` creates the Locktivity release.

### Pull

1. `pull.prepare` resolves a release reference.
2. The adapter fetches a download URL for the associated pack.
3. `pull.finalize` confirms completion to the caller.

Supported pull refs today:
- Latest release (`epack pull locktivity`)
- Release ID (`epack pull locktivity --release <id>`)
- Version lookup (`epack pull locktivity --version ...`)
- Digest lookup (`epack pull locktivity --digest ...`)

Lookup scoping currently uses `target.environment` from the remote target.

### Run Sync

`runs.sync` posts run IDs, result paths, and digests for a given pack digest.

## Authentication

Current user-facing auth mode is client credentials only:

```bash
export LOCKTIVITY_CLIENT_ID="your-client-id"
export LOCKTIVITY_CLIENT_SECRET="your-client-secret"
```

OIDC and device-code login are not part of the current user-facing setup.

## Security Model

- API calls use HTTPS.
- Upload/download transfer uses time-limited presigned URLs.

## Limits

| Limit | Value |
|-------|-------|
| Max pack size | 100 MB |
