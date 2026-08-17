# Security Policy

## Supported Versions

Only the latest released minor version of `ggvalet` receives security updates.
Older versions are not maintained; upgrade to the current release to stay protected.

| Version | Supported |
|--------|----------|
| latest | yes |
| older  | no |

## Reporting a Vulnerability

If you believe you have found a security vulnerability in `ggvalet`, please report
it responsibly. Do not open a public issue for security bugs; use a private
channel or the project's security advisory process.

- **Subject:** `[SECURITY] ggvalet - <short description>`
- **Include:** a clear description, steps to reproduce, affected versions, and any
  proof-of-concept or logs you can share.

The maintainer will acknowledge receipt within 5 business days and provide an
initial assessment within 10 business days. Coordinated disclosure is preferred:
we will work with you to fix the issue and disclose it after a patch is released.

## Security-Related Checks

The project runs the following automated checks on every change:

- `go vet ./...` — static analysis
- `govulncheck ./...` — Go vulnerability database scan
- `gitleaks detect` — secret leakage detection in CI
- `make cover` — per-package coverage gates
- `make vwp` — VWP marketing/stub-surfacing lint

## Supply-Chain Verification

Signed release artifacts include a `SHA256SUMS` file, a cosign keyless signature
(`SHA256SUMS.sig`), and a certificate (`SHA256SUMS.pem`). Each release also ships
a CycloneDX SBOM (`sbom.cdx.json`). See [Verify](../verify.md) for instructions.
