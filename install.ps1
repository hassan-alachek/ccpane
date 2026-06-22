# ccpane installer for Windows (PowerShell).
#
#   irm https://raw.githubusercontent.com/hassan-alachek/ccpane/main/install.ps1 | iex
#
# Env overrides:
#   $env:CCPANE_REPO         owner/repo          (default: hassan-alachek/ccpane)
#   $env:CCPANE_VERSION      vX.Y.Z             (default: latest release)
#   $env:CCPANE_INSTALL_DIR  install directory  (default: %LOCALAPPDATA%\Programs\ccpane)

$ErrorActionPreference = 'Stop'
$Repo = if ($env:CCPANE_REPO) { $env:CCPANE_REPO } else { 'hassan-alachek/ccpane' }
$Bin  = 'ccpane'

$arch = switch ($env:PROCESSOR_ARCHITECTURE) {
  'AMD64' { 'amd64' }
  'ARM64' { 'arm64' }
  default { throw "unsupported architecture: $env:PROCESSOR_ARCHITECTURE" }
}

$asset    = "${Bin}_windows_${arch}.zip"
$releases = "https://github.com/$Repo/releases"
$path     = if ($env:CCPANE_VERSION) { "download/$($env:CCPANE_VERSION)" } else { "latest/download" }
$url      = "$releases/$path/$asset"

$tmp = Join-Path $env:TEMP ("ccpane-" + [guid]::NewGuid().ToString())
New-Item -ItemType Directory -Path $tmp -Force | Out-Null
try {
  $zip = Join-Path $tmp $asset
  Write-Host "==> downloading $asset" -ForegroundColor Cyan
  Invoke-WebRequest -Uri $url -OutFile $zip -UseBasicParsing

  # verify checksum (best effort)
  try {
    $sums = (Invoke-WebRequest -Uri "$releases/$path/checksums.txt" -UseBasicParsing).Content
    $line = ($sums -split "`n" | Where-Object { $_ -match [regex]::Escape($asset) } | Select-Object -First 1)
    if ($line) {
      $want = ($line -split '\s+')[0].ToLower()
      $got  = (Get-FileHash -Algorithm SHA256 $zip).Hash.ToLower()
      if ($want -ne $got) { throw "checksum mismatch (expected $want, got $got)" }
      Write-Host "==> checksum verified" -ForegroundColor Cyan
    }
  } catch { Write-Warning "checksum verification skipped: $_" }

  Expand-Archive -Path $zip -DestinationPath $tmp -Force

  $dir = if ($env:CCPANE_INSTALL_DIR) { $env:CCPANE_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA 'Programs\ccpane' }
  New-Item -ItemType Directory -Path $dir -Force | Out-Null
  Copy-Item -Path (Join-Path $tmp "$Bin.exe") -Destination (Join-Path $dir "$Bin.exe") -Force
  Write-Host "==> installed to $dir\$Bin.exe" -ForegroundColor Cyan

  # add to user PATH
  $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
  if (-not ($userPath -split ';' | Where-Object { $_ -eq $dir })) {
    [Environment]::SetEnvironmentVariable('Path', ($userPath.TrimEnd(';') + ";$dir"), 'User')
    Write-Host "==> added $dir to your user PATH (open a new terminal to use it)" -ForegroundColor Cyan
  }
  Write-Host "==> done. Run: ccpane" -ForegroundColor Green
}
finally {
  Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}
