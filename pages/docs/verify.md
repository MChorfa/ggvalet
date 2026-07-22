# Verify a release

Every tagged release ships with:

- Cross-platform archives
- `SHA256SUMS`
- `SHA256SUMS.sig` (cosign keyless signature)
- `SHA256SUMS.pem` (cosign keyless certificate)
- CycloneDX SBOM (`sbom.cdx.json`)

Download the archive, `SHA256SUMS`, `SHA256SUMS.sig`, and `SHA256SUMS.pem` for the
release you want, then extract the binary:

```bash
tar -xzf glv_*.tar.gz
RELEASE_TAG=$(./ggvalet --version | awk '{print $2}')
```

## Checksums

```bash
sha256sum -c SHA256SUMS
```

On macOS, `sha256sum` may not be installed; use `shasum` instead:

```bash
shasum -a 256 -c SHA256SUMS
```

## Cosign signature

```bash
RELEASE_PROJECT_URL="https://github.com/MChorfa/ggvalet"
RELEASE_ISSUER="https://token.actions.githubusercontent.com"

cosign verify-blob SHA256SUMS \
  --signature SHA256SUMS.sig \
  --certificate SHA256SUMS.pem \
  --certificate-identity "${RELEASE_PROJECT_URL}//.github/workflows/release.yml@refs/tags/${RELEASE_TAG}" \
  --certificate-oidc-issuer "${RELEASE_ISSUER}"
```

For GitLab releases, set the issuer to your GitLab instance URL and the identity to:

```bash
"${CI_PROJECT_URL}//.gitlab-ci.yml@refs/tags/${CI_COMMIT_TAG}"
```

## SBOM

The `sbom.cdx.json` file is a CycloneDX SBOM produced by Syft. You can inspect
it with `syft` or any CycloneDX-compatible tool.
