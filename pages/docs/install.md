# Install

## One-line installer

Download and install the latest release for your platform:

```bash
curl -L https://MChorfa.github.io/ggvalet/install.sh | bash
```

Use a different install directory:

```bash
curl -L https://MChorfa.github.io/ggvalet/install.sh | INSTALL_DIR=~/.local/bin bash
```

The installer detects your OS and architecture, queries the GitHub latest release
API, downloads the matching archive, verifies the SHA-256 checksum, and installs
`ggvalet`.

## Manual install

### macOS / Linux

Download the archive for your platform and architecture and install it:

```bash
os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')

tag=$(curl -fsL https://api.github.com/repos/MChorfa/ggvalet/releases/latest \
  | python3 -c "import sys,json; print(json.load(sys.stdin)['tag_name'])")

curl -LO "https://github.com/MChorfa/ggvalet/releases/download/${tag}/ggvalet_${tag}_${os}_${arch}.tar.gz"
tar -xzf "ggvalet_${tag}_${os}_${arch}.tar.gz"
sudo install ggvalet /usr/local/bin/
```

### Windows

Download and install the latest release with PowerShell:

```powershell
iwr -useb https://MChorfa.github.io/ggvalet/install.ps1 | iex
```

Or, if you prefer to install manually, download `ggvalet_<version>_windows_amd64.zip`
from the [Releases](releases.md) page, extract `ggvalet.exe`, and place it in a
directory on your `PATH`.

## First run

```bash
ggvalet --version
ggvalet doctor
ggvalet hosts
```

See the project [README](README.md) for a full feature map and configuration
options.
