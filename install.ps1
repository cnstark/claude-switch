param(
    [string]$InstallDir = "$env:USERPROFILE\.claude_switch\bin"
)

$ErrorActionPreference = "Stop"

$Repo = "cnstark/claude-switch"
$EnvFile = "$env:USERPROFILE\.claude_switch\env.ps1"

# --- Detect arch ---
$Arch = if ([Environment]::Is64BitOperatingSystem) { "amd64" } else { "x86" }
if ($Arch -ne "amd64") {
    Write-Host "Unsupported architecture: $Arch (64-bit only)"
    exit 1
}

$OS = "windows"

# --- Get latest version ---
Write-Host "==> Fetching latest version..."
$ReleaseInfo = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/latest"
$Version = $ReleaseInfo.tag_name
Write-Host "==> Latest version: $Version"

# --- Download and extract ---
$PkgBase = "claude-switch_${Version}_${OS}_${Arch}"
$PkgFile = "${PkgBase}.zip"
$DownloadUrl = "https://github.com/$Repo/releases/download/$Version/$PkgFile"

Write-Host "==> Downloading $PkgFile ..."
$TmpDir = Join-Path $env:TEMP "claude-switch-$(Get-Random)"
New-Item -ItemType Directory -Force -Path $TmpDir | Out-Null
$ZipPath = Join-Path $TmpDir $PkgFile

Invoke-WebRequest -Uri $DownloadUrl -OutFile $ZipPath

Write-Host "==> Installing to $InstallDir ..."
New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
Expand-Archive -Path $ZipPath -DestinationPath $TmpDir -Force

Copy-Item -Path "$TmpDir\$PkgBase\cs.exe" -Destination $InstallDir -Force
Copy-Item -Path "$TmpDir\$PkgBase\cs-proxy.exe" -Destination $InstallDir -Force

Remove-Item -Recurse -Force $TmpDir

# --- Generate env.ps1 ---
@"
# claude-switch environment - dot-source this file to add binaries to PATH
`$env:PATH = "$InstallDir;`$env:PATH"
"@ | Out-File -FilePath $EnvFile -Encoding utf8

# --- Verify ---
$CsPath = Join-Path $InstallDir "cs.exe"
if (Test-Path $CsPath) {
    $InstalledVer = (& $CsPath version | Select-Object -Last 1) -replace "cs version ", ""
    Write-Host ""
    Write-Host "✅ Installation successful! Version: $InstalledVer"
} else {
    Write-Host ""
    Write-Host "✅ Installation successful!"
}

Write-Host ""
Write-Host "To use now (current PowerShell):"
Write-Host "  & `$env:USERPROFILE\.claude_switch\env.ps1"
Write-Host ""
Write-Host "To make permanent (add to PowerShell Profile):"
Write-Host "  echo '& `$env:USERPROFILE\.claude_switch\env.ps1' >> `$PROFILE"
Write-Host ""
Write-Host "Quick start:"
Write-Host "  cs help"
