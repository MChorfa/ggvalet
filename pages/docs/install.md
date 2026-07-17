# Install

## One-line installer

Download and install the latest release for your platform:

```bash
curl -L https://ckodex.github.io/gitlabvalet/install.sh | bash
```

Use a different install directory:

```bash
curl -L https://ckodex.github.io/gitlabvalet/install.sh | INSTALL_DIR=~/.local/bin bash
```

The installer detects your OS and architecture, downloads the matching archive
from GitHub Releases, verifies the SHA-256 checksum, and installs `glv`.

## Manual install

If you prefer to install manually, pick the archive for your platform from the
[Releases](releases.md) page and run:

```bash
os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')
# Replace <tag> with the release you want from the Releases page.
tag=<tag>
curl -LO "https://github.com/ckodex/gitlabvalet/releases/download/${tag}/glv_${tag}_${os}_${arch}.tar.gz"
tar -xzf "glv_${tag}_${os}_${arch}.tar.gz"
sudo install glv /usr/local/bin/
```

## Windows

Download `glv_<tag>_windows_amd64.zip` from the [Releases](releases.md) page,
extract `glv.exe`, and place it in a directory on your `PATH`.

## First run

```bash
glv --version
glv hosts
```

See the project [README](README.md) for a full feature map and configuration
options.
