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

The installer detects your OS and architecture, queries the GitHub latest release
API, downloads the matching archive, verifies the SHA-256 checksum, and installs
`glv`.

## Manual install

### macOS / Linux

Download the archive for your platform and architecture and install it:

```bash
os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')

# Uses the latest release by default.
tag=$(curl -fsL https://api.github.com/repos/ckodex/gitlabvalet/releases/latest \
  | python3 -c "import sys,json; print(json.load(sys.stdin)['tag_name'])")

curl -LO "https://github.com/ckodex/gitlabvalet/releases/download/${tag}/glv_${tag}_${os}_${arch}.tar.gz"
tar -xzf "glv_${tag}_${os}_${arch}.tar.gz"
sudo install glv /usr/local/bin/
```

### Windows

Download and install the latest release with PowerShell:

```powershell
iwr -useb https://ckodex.github.io/gitlabvalet/install.ps1 | iex
```

Or, if you prefer to install manually, download `glv_<version>_windows_amd64.zip`
from the [Releases](releases.md) page, extract `glv.exe`, and place it in a
directory on your `PATH`.

## First run

```bash
glv --version
glv hosts
```

See the project [README](README.md) for a full feature map and configuration
options.
