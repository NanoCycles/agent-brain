param(
    [string]$Repo = $env:AGENT_BRAIN_REPO,
    [string]$Version = $env:AGENT_BRAIN_VERSION,
    [string]$InstallDir = $env:AGENT_BRAIN_INSTALL_DIR
)

if (-not $Repo) { $Repo = "NanoCycles/agent-brain" }
if (-not $Version) { $Version = "latest" }
if (-not $InstallDir) { $InstallDir = Join-Path $HOME ".agent-brain\bin" }

$arch = if ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture -eq "Arm64") { "arm64" } else { "amd64" }
$asset = "agent-brain_windows_$arch.zip"
if ($Version -eq "latest") {
    $url = "https://github.com/$Repo/releases/latest/download/$asset"
} else {
    $url = "https://github.com/$Repo/releases/download/$Version/$asset"
}

$tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("agent-brain-" + [System.Guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Force -Path $tmp | Out-Null
New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null

try {
    $zip = Join-Path $tmp $asset
    Write-Host "Downloading $url"
    Invoke-WebRequest -Uri $url -OutFile $zip
    Expand-Archive -Path $zip -DestinationPath $tmp -Force
    Copy-Item -Path (Join-Path $tmp "agent-brain.exe") -Destination (Join-Path $InstallDir "agent-brain.exe") -Force
    Write-Host "agent-brain installed at $InstallDir\agent-brain.exe"
    Write-Host "Add $InstallDir to PATH if needed."
} finally {
    Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}
