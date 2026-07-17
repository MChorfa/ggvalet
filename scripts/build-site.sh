#!/usr/bin/env bash
# Build the GitLab Valet static site from pages/, README.md, and optional
# GoReleaser metadata/artifacts JSON. The output directory is suitable for
# GitHub Pages or GitLab Pages (use --output public for GitLab).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUTDIR="$ROOT/site"
METADATA=""
ARTIFACTS=""
VERSION="latest"
GITHUB_OWNER="ckodex"
GITHUB_REPO="gitlabvalet"
GITLAB_URL="https://gitlab.com/ckodex/gitlabvalet"
RELEASE_BASE_URL=""

usage() {
  cat <<EOF
Usage: $(basename "$0") [options]

Options:
  --output <dir>           Output directory (default: $ROOT/site)
  --metadata <path>        Path to GoReleaser dist/metadata.json
  --artifacts <path>       Path to GoReleaser dist/artifacts.json
  --version <version>      Fallback release version (default: latest)
  --github-owner <owner>   GitHub owner (default: $GITHUB_OWNER)
  --github-repo <repo>     GitHub repo (default: $GITHUB_REPO)
  --gitlab-url <url>       GitLab project base URL (default: $GITLAB_URL)
  --release-base-url <url> Base URL for artifact downloads.
                           Defaults to the GitHub release download URL.
  -h, --help               Show this help
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --output)
      OUTDIR="$2"
      shift 2
      ;;
    --metadata)
      METADATA="$2"
      shift 2
      ;;
    --artifacts)
      ARTIFACTS="$2"
      shift 2
      ;;
    --version)
      VERSION="$2"
      shift 2
      ;;
    --github-owner)
      GITHUB_OWNER="$2"
      shift 2
      ;;
    --github-repo)
      GITHUB_REPO="$2"
      shift 2
      ;;
    --gitlab-url)
      GITLAB_URL="$2"
      shift 2
      ;;
    --release-base-url)
      RELEASE_BASE_URL="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown option: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

rm -rf "$OUTDIR"
mkdir -p "$OUTDIR"

cp -R "$ROOT/pages/." "$OUTDIR/"
mkdir -p "$OUTDIR/docs"
cp "$ROOT/README.md" "$OUTDIR/docs/README.md"

# Include the installers in the site root.
if [[ -f "$ROOT/scripts/install-glv.sh" ]]; then
  cp "$ROOT/scripts/install-glv.sh" "$OUTDIR/install.sh"
fi
if [[ -f "$ROOT/scripts/install-glv.ps1" ]]; then
  cp "$ROOT/scripts/install-glv.ps1" "$OUTDIR/install.ps1"
fi

# Include the in-repo docs/ as a sub-section of the site.
if [[ -d "$ROOT/docs" ]]; then
  cp -R "$ROOT/docs" "$OUTDIR/docs/project-docs"
fi

RELEASE_MD="$OUTDIR/docs/releases.md"

generate_generic() {
  cat > "$RELEASE_MD" <<EOF
# Releases

Tagged releases publish cross-platform archives, a signed \`SHA256SUMS\`, a
cosign keyless certificate/Signature pair, and a CycloneDX SBOM.

- [GitHub Releases](https://github.com/$GITHUB_OWNER/$GITHUB_REPO/releases/latest)
- [GitLab Releases]($GITLAB_URL/-/releases)

See [Install](install.md) and [Verify](verify.md) for usage details.
EOF
}

generate_from_metadata() {
  [[ -n "$METADATA" ]] && cp "$METADATA" "$OUTDIR/metadata.json"
  [[ -n "$ARTIFACTS" ]] && cp "$ARTIFACTS" "$OUTDIR/artifacts.json"

  python3 - "$METADATA" "$ARTIFACTS" "$VERSION" "$GITHUB_OWNER" "$GITHUB_REPO" "$GITLAB_URL" "$RELEASE_BASE_URL" <<'PY'
import json
import sys

meta_path, artifacts_path, version, owner, repo, gl_url, release_base = sys.argv[1:8]

meta = {}
if meta_path:
    with open(meta_path) as f:
        meta = json.load(f)

artifacts = []
if artifacts_path:
    with open(artifacts_path) as f:
        artifacts = json.load(f)

version = meta.get("version") or version
if not release_base:
    release_base = f"https://github.com/{owner}/{repo}/releases/download/{version}/"

out = []
out.append("# Releases")
out.append("")
out.append(f"## {version}")
out.append("")
out.append("| Artifact | OS | Arch | Download |")
out.append("|----------|----|------|----------|")

for a in artifacts:
    name = a.get("name", "")
    typ = a.get("type") or a.get("internal_type") or ""
    if not name:
        continue
    if typ in ("Source", "Metadata", "Binary"):
        continue

    goos = a.get("goos", "-")
    goarch = a.get("goarch", "-")
    if typ == "Archive":
        url = release_base + name
        out.append(f"| `{name}` | {goos} | {goarch} | [download]({url}) |")
    elif typ in ("Checksum", "Signature", "Certificate") or name.endswith((".cdx.json", ".sig", ".pem")) or "sbom" in name.lower():
        url = release_base + name
        out.append(f"| `{name}` | - | - | [download]({url}) |")

out.append("")
out.append(f"- [GitHub release](https://github.com/{owner}/{repo}/releases/tag/{version})")
out.append(f"- [GitLab release]({gl_url}/-/releases/{version})")
out.append("")
out.append("### Verification")
out.append("")
out.append("Download `SHA256SUMS`, `SHA256SUMS.sig`, and `SHA256SUMS.pem`, then run:")
out.append("")
out.append("```bash")
out.append("cosign verify-blob SHA256SUMS \\")
out.append("  --signature SHA256SUMS.sig \\")
out.append("  --certificate SHA256SUMS.pem \\")
out.append(f"  --certificate-identity \"{owner}/{repo}//.github/workflows/release.yml@refs/tags/{version}\" \\")
out.append("  --certificate-oidc-issuer https://token.actions.githubusercontent.com")
out.append("```")
out.append("")
out.append("For GitLab releases, use your GitLab instance URL as the issuer and the CI identity.")

print("\n".join(out))
PY
}

if [[ -n "$ARTIFACTS" && -f "$ARTIFACTS" ]]; then
  generate_from_metadata > "$RELEASE_MD"
else
  generate_generic
fi

echo "site built: $OUTDIR"
