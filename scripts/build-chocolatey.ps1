param(
    [string]$Version,
    [string]$ChecksumsPath,
    [string]$OutputDir = "dist\chocolatey"
)

$ErrorActionPreference = "Stop"

if (-not $Version) {
    $Version = (git describe --tags --exact-match 2>$null)
    if (-not $Version) {
        throw "Version is required when HEAD is not exactly on a tag."
    }
}
$Version = $Version.TrimStart("v")

if (-not $ChecksumsPath) {
    $ChecksumsPath = Join-Path "dist" "checksums.txt"
}
if (-not (Test-Path $ChecksumsPath)) {
    throw "Checksums file not found: $ChecksumsPath"
}

$checksums = Get-Content $ChecksumsPath
$amd64 = ($checksums | Where-Object { $_ -match "agent-brain_windows_amd64\.zip$" } | ForEach-Object { ($_ -split "\s+")[0] } | Select-Object -First 1)
$arm64 = ($checksums | Where-Object { $_ -match "agent-brain_windows_arm64\.zip$" } | ForEach-Object { ($_ -split "\s+")[0] } | Select-Object -First 1)

if (-not $amd64) { throw "Missing amd64 checksum." }
if (-not $arm64) { throw "Missing arm64 checksum." }

$packageRoot = Join-Path $OutputDir "agent-brain"
$toolsDir = Join-Path $packageRoot "tools"
Remove-Item -Recurse -Force $packageRoot -ErrorAction SilentlyContinue
New-Item -ItemType Directory -Force -Path $toolsDir | Out-Null

$nuspecTemplate = Join-Path "packaging\chocolatey\agent-brain" "agent-brain.nuspec.tpl"
$installTemplate = Join-Path "packaging\chocolatey\agent-brain\tools" "chocolateyinstall.ps1.tpl"

$nuspec = (Get-Content $nuspecTemplate -Raw).Replace("__VERSION__", $Version)
Set-Content -Path (Join-Path $packageRoot "agent-brain.nuspec") -Value $nuspec -Encoding UTF8

$install = (Get-Content $installTemplate -Raw).
    Replace("__VERSION__", $Version).
    Replace("__CHECKSUM_AMD64__", $amd64).
    Replace("__CHECKSUM_ARM64__", $arm64)
Set-Content -Path (Join-Path $toolsDir "chocolateyinstall.ps1") -Value $install -Encoding UTF8

Push-Location $packageRoot
try {
    choco pack
} finally {
    Pop-Location
}

$nupkg = Get-ChildItem $packageRoot -Filter "*.nupkg" | Select-Object -First 1
if (-not $nupkg) {
    throw "Chocolatey package was not created."
}

Write-Host $nupkg.FullName
