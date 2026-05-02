$ErrorActionPreference = 'Stop'

$packageName = 'agent-brain'
$toolsDir = Split-Path -Parent $MyInvocation.MyCommand.Definition
$url64 = 'https://github.com/NanoCycles/agent-brain/releases/download/v__VERSION__/agent-brain_windows_amd64.zip'
$urlArm64 = 'https://github.com/NanoCycles/agent-brain/releases/download/v__VERSION__/agent-brain_windows_arm64.zip'
$checksum64 = '__CHECKSUM_AMD64__'
$checksumArm64 = '__CHECKSUM_ARM64__'

$isArm64 = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture -eq [System.Runtime.InteropServices.Architecture]::Arm64
if ($isArm64) {
  Install-ChocolateyZipPackage `
    -PackageName $packageName `
    -Url64bit $urlArm64 `
    -UnzipLocation $toolsDir `
    -Checksum64 $checksumArm64 `
    -ChecksumType64 'sha256'
} else {
  Install-ChocolateyZipPackage `
    -PackageName $packageName `
    -Url64bit $url64 `
    -UnzipLocation $toolsDir `
    -Checksum64 $checksum64 `
    -ChecksumType64 'sha256'
}
