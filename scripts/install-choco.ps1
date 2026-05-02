param(
    [string]$Repo = $env:AGENT_BRAIN_REPO,
    [string]$Version = $env:AGENT_BRAIN_VERSION
)

$ErrorActionPreference = "Stop"

if (-not $Repo) { $Repo = "NanoCycles/agent-brain" }

if (-not (Get-Command choco -ErrorAction SilentlyContinue)) {
    throw "Chocolatey is not installed. Install it from https://chocolatey.org/install, then rerun this script."
}

if (-not $Version -or $Version -eq "latest") {
    $release = Invoke-RestMethod "https://api.github.com/repos/$Repo/releases/latest"
    $Version = $release.tag_name
} else {
    $release = Invoke-RestMethod "https://api.github.com/repos/$Repo/releases/tags/$Version"
}

$asset = $release.assets | Where-Object { $_.name -like "agent-brain.*.nupkg" } | Select-Object -First 1
if (-not $asset) {
    throw "No Chocolatey package asset was found on release $Version."
}

$tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("agent-brain-choco-" + [System.Guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Force -Path $tmp | Out-Null

try {
    $nupkg = Join-Path $tmp $asset.name
    Write-Host "Downloading $($asset.browser_download_url)"
    Invoke-WebRequest -Uri $asset.browser_download_url -OutFile $nupkg
    choco install agent-brain --source $tmp --version $Version.TrimStart("v") -y
} finally {
    Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}
