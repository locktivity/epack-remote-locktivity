# Locktivity Remote Examples

## Basic Push/Pull

```yaml
# epack.yaml
remotes:
  locktivity:
    adapter: locktivity
    source: locktivity/epack-remote-locktivity@v1
    secrets:
      - LOCKTIVITY_CLIENT_ID
      - LOCKTIVITY_CLIENT_SECRET
```

```bash
# Push a local pack
epack push locktivity packs/evidence.epack

# Pull latest release
epack pull locktivity
```

## Pull by Release ID

```bash
epack pull locktivity --release rel_abc123
```

## Collector Run + Push

```bash
# Build a pack from configured collectors
epack collector run --output packs/evidence.epack

# Push pack (run sync is handled during push unless --no-runs is set)
epack push locktivity packs/evidence.epack
```

## Environment-Specific Remotes

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

```bash
epack push locktivity-staging packs/evidence.epack
epack push locktivity-prod packs/evidence.epack
```

## Client Credentials in CI

```yaml
jobs:
  push:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Build pack
        run: epack collector run --output packs/evidence.epack

      - name: Push pack
        env:
          LOCKTIVITY_CLIENT_ID: ${{ secrets.LOCKTIVITY_CLIENT_ID }}
          LOCKTIVITY_CLIENT_SECRET: ${{ secrets.LOCKTIVITY_CLIENT_SECRET }}
        run: epack push locktivity packs/evidence.epack
```
