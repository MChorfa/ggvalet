#!/usr/bin/env bash
# Install the latest ggvalet release for your OS and architecture.
# Usage:
#   curl -L https://MChorfa.github.io/ggvalet/install.sh | bash
#   curl -L https://MChorfa.github.io/ggvalet/install.sh | INSTALL_DIR=~/.local/bin bash
set -euo pipefail

GITHUB_REPO="${GLV_GITHUB_REPO:-MChorfa/ggvalet}"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

die() {
  echo "$1" >&2
  exit 1
}

get_latest_tag() {
  local tag
  tag=$(curl -fsL "https://api.github.com/repos/${GITHUB_REPO}/releases/latest" \
    | python3 -c "import sys,json; print(json.load(sys.stdin).get('tag_name',''))")
  if [[ -z "$tag" ]]; then
    die "Could not determine latest release tag."
  fi
  echo "$tag"
}

os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/;s/arm64/arm64/')

case "$os" in
  darwin|linux) ;;
  *) die "Unsupported OS: $os" ;;
esac

case "$arch" in
  amd64|arm64) ;;
  *) die "Unsupported architecture: $arch" ;;
esac

tag=${1:-$(get_latest_tag)}
archive="ggvalet_${tag}_${os}_${arch}.tar.gz"
url="https://github.com/${GITHUB_REPO}/releases/download/${tag}/${archive}"
sha_url="https://github.com/${GITHUB_REPO}/releases/download/${tag}/SHA256SUMS"

tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT
cd "$tmpdir"

echo "Downloading ${archive}..."
curl -fL -o "$archive" "$url"
echo "Downloading SHA256SUMS..."
curl -fL -o SHA256SUMS "$sha_url"

if command -v sha256sum >/dev/null 2>&1; then
  grep "${archive}" SHA256SUMS | sha256sum -c -
elif command -v shasum >/dev/null 2>&1; then
  grep "${archive}" SHA256SUMS | shasum -a 256 -c -
else
  echo "Warning: neither sha256sum nor shasum found; skipping checksum verification" >&2
fi

tar -xzf "$archive"

echo "Installing ggvalet to ${INSTALL_DIR}..."
mkdir -p "$INSTALL_DIR"
install -m 755 ggvalet "${INSTALL_DIR}/ggvalet"
echo "Done. Run 'ggvalet --version' to confirm."
