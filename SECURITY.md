# Security Policy

## Supported Versions

Security fixes target the latest released version of `kubectl-hpa-status`.
Users should prefer the newest release from GitHub Releases, Krew, or Homebrew.

## Reporting a Vulnerability

Please report security issues privately through GitHub Security Advisories when
available. If that is not possible, open a minimal public issue without exploit
details and request a private contact path.

Include:

- affected version or commit
- operating system and Kubernetes version
- whether the issue requires cluster credentials
- minimal reproduction details

## Security Model

The plugin uses the user's existing kubeconfig credentials and does not run a
server. It reads HPA objects and Events by default. Mutating behavior is limited
to explicit `--apply` workflows.

Safety rules for mutating workflows:

- `--suggest` emits dry-run patch commands.
- `--apply` defaults to server-side dry-run.
- persistent changes require `--dry-run=false`.
- confirmation is required unless `-y` is explicitly provided.

## Supply Chain

The release pipeline uses GoReleaser and generates:

- **Checksums** (`checksums.txt`) for every archive
- **SBOM** metadata for release archives
- **Cosign keyless signatures** (sigstore) for archives and checksums
- **SLSA build provenance** attestations attached to the GitHub Release

CI also runs tests, linting, govulncheck, gosec, and CodeQL.

### Verifying a release artifact

After downloading an archive and its `.sig` / `.pem` companions from the
GitHub Release:

```sh
# Cosign keyless signature (certificate identity is the release workflow)
cosign verify-blob \
  --certificate <artifact>.pem \
  --signature <artifact>.sig \
  --certificate-identity-regexp 'https://github.com/mattsu2020/kubectl-hpa-status/' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  <artifact>

# Optional: inspect SLSA provenance attached to the release
gh attestation verify <artifact> --repo mattsu2020/kubectl-hpa-status
```

Exact certificate identity strings may evolve with the release workflow; prefer
the identity values published in the release notes when present.
