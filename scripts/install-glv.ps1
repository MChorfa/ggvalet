# Install the latest GitLab Valet release for Windows.
# Usage:
#   iwr -useb https://ckodex.github.io/gitlabvalet/install.ps1 | iex

$ErrorActionPreference = "Stop"

$repo = if ($env:GLV_GITHUB_REPO) { $env:GLV_GITHUB_REPO } else { "ckodex/gitlabvalet" }

$arch = if ([Environment]::Is64BitOperatingSystem) { "amd64" } else { "amd64" }
$tag = $args[0]

if (-not $tag) {
    $release = Invoke-RestMethod -Uri "https://api.github.com/repos/$repo/releases/latest" -UseBasicParsing
    $tag = $release.tag_name
}

if (-not $tag) {
    throw "Could not determine latest release tag."
}

$archive = "glv_${tag}_windows_${arch}.zip"
$url = "https://github.com/$repo/releases/download/$tag/$archive"
$shaUrl = "https://github.com/$repo/releases/download/$tag/SHA256SUMS"

$tmp = New-TemporaryFile | Select-Object -ExpandProperty FullName
Remove-Item $tmp
New-Item -ItemType Directory -Path $tmp | Out-Null

try {
    $archivePath = Join-Path $tmp $archive
    $shaPath = Join-Path $tmp "SHA256SUMS"

    Write-Host "Downloading $archive..."
    Invoke-WebRequest -Uri $url -OutFile $archivePath -UseBasicParsing

    Write-Host "Downloading SHA256SUMS..."
    Invoke-WebRequest -Uri $shaUrl -OutFile $shaPath -UseBasicParsing

    $archiveHash = (Get-FileHash -Algorithm SHA256 -Path $archivePath).Hash.ToLower()
    $shaLine = Select-String -Path $shaPath -Pattern $archive | Select-Object -First 1
    if (-not $shaLine) {
        throw "Could not find $archive in SHA256SUMS."
    }
    $expectedHash = ($shaLine.Line -split '\s+')[0]
    if ($archiveHash -ne $expectedHash) {
        throw "SHA-256 checksum mismatch. Expected $expectedHash, got $archiveHash."
    }

    Expand-Archive -Path $archivePath -DestinationPath $tmp -Force

    $installDir = $env:INSTALL_DIR
    if (-not $installDir) {
        $installDir = "$env:LOCALAPPDATA\Microsoft\WindowsApps"
    }
    if (-not (Test-Path $installDir)) {
        New-Item -ItemType Directory -Path $installDir -Force | Out-Null
    }

    Copy-Item -Path (Join-Path $tmp "glv.exe") -Destination (Join-Path $installDir "glv.exe") -Force
    Write-Host "glv installed to $installDir\glv.exe"
} finally {
    Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}
