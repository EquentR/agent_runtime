[CmdletBinding()]
param(
  [ValidateSet('install','uninstall','start','stop','restart','status','dry-run')]
  [string]$Action = 'install',
  [string]$InstallDir = '',
  [string]$ServiceName = 'IceArt',
  [string]$ServiceAccount = 'NT AUTHORITY\LocalService',
  [switch]$Purge,
  [string]$ConfirmPurge = ''
)
$ErrorActionPreference = 'Stop'
$validServiceName = '^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$'
if ($ServiceName -notmatch $validServiceName) { throw 'invalid service name' }
$allowedAccounts = @('NT AUTHORITY\LocalService', 'NT AUTHORITY\NetworkService', 'LocalSystem')
if ($ServiceAccount -notin $allowedAccounts) { throw 'service account must be LocalService, NetworkService, or LocalSystem' }
$accountSID = switch ($ServiceAccount) {
  'NT AUTHORITY\LocalService' { 'LS' }
  'NT AUTHORITY\NetworkService' { 'NS' }
  default { 'SY' }
}
if ($InstallDir.Contains("`r") -or $InstallDir.Contains("`n")) { throw 'invalid install directory' }
$isDryRun = $Action -eq 'dry-run'
if ($isDryRun) { $Action = 'install' }
$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
if ([string]::IsNullOrWhiteSpace($InstallDir)) { $InstallDir = Split-Path -Parent $scriptDir }
$InstallDir = [IO.Path]::GetFullPath($InstallDir)
$exe = Join-Path $InstallDir 'ice_art.exe'
$config = Join-Path $InstallDir 'conf\app.yaml'
$marker = Join-Path $InstallDir '.ice-art-install-root'
function Invoke-Step([string]$File, [string[]]$Arguments) {
  if ($isDryRun) { Write-Host '+ ' $File ($Arguments -join ' '); return }
  & $File @Arguments
  if ($LASTEXITCODE -ne 0) { throw "$File failed with exit code $LASTEXITCODE" }
}
switch ($Action) {
  'install' {
    if (!(Test-Path -LiteralPath $exe)) { throw "missing executable: $exe" }
    if (!(Test-Path -LiteralPath $config)) { throw "missing config: $config" }
    $binPath = '"' + $exe + '" -config "' + $config + '" -runtime-mode windows-service -service-name "' + $ServiceName + '"'
    if (!$isDryRun) {
      $existing = Get-Service -Name $ServiceName -ErrorAction SilentlyContinue
      if ($existing) { Stop-Service -Name $ServiceName -ErrorAction SilentlyContinue; sc.exe delete $ServiceName | Out-Null; Start-Sleep -Milliseconds 500 }
    }
    Invoke-Step 'sc.exe' @('create', $ServiceName, 'binPath=', $binPath, 'start=', 'auto', 'obj=', $ServiceAccount, 'DisplayName=', 'Ice Art')
    Invoke-Step 'sc.exe' @('description', $ServiceName, 'Ice Art agent runtime')
    if ($ServiceAccount -ne 'LocalSystem') {
      Invoke-Step 'icacls.exe' @($InstallDir, '/grant', "${ServiceAccount}:(OI)(CI)M", '/T', '/C')
    }
    $serviceDACL = 'D:(A;;CCLCSWRPWPDTLOCRRC;;;SY)(A;;CCDCLCSWRPWPDTLOCRSDRCWDWO;;;BA)'
    if ($accountSID -ne 'SY') { $serviceDACL += "(A;;CCLCSWRPWPDTLOCRRC;;;$accountSID)" }
    Invoke-Step 'sc.exe' @('sdset', $ServiceName, $serviceDACL)
    if (!$isDryRun) { Start-Service -Name $ServiceName }
    if ($isDryRun) { Write-Host "+ write ownership marker $marker" } else { [IO.File]::WriteAllText($marker, $InstallDir + [Environment]::NewLine) }
  }
  'uninstall' {
    if (!$isDryRun) { Stop-Service -Name $ServiceName -ErrorAction SilentlyContinue }
    Invoke-Step 'sc.exe' @('delete', $ServiceName)
    if ($Purge) {
      if ($ConfirmPurge -ne $InstallDir -or $InstallDir -eq [IO.Path]::GetPathRoot($InstallDir)) { throw "ConfirmPurge must exactly match $InstallDir" }
      if (!(Test-Path -LiteralPath $marker) -or (Get-Content -LiteralPath $marker -Raw).Trim() -ne $InstallDir) { throw 'install ownership marker is missing or invalid' }
      if ($isDryRun) { Write-Host "+ Remove-Item -Recurse -LiteralPath $InstallDir" } else { Remove-Item -Recurse -Force -LiteralPath $InstallDir }
    }
  }
  'start' { if ($isDryRun) { Write-Host "+ Start-Service $ServiceName" } else { Start-Service -Name $ServiceName } }
  'stop' { if ($isDryRun) { Write-Host "+ Stop-Service $ServiceName" } else { Stop-Service -Name $ServiceName } }
  'restart' { if ($isDryRun) { Write-Host "+ Restart-Service $ServiceName" } else { Restart-Service -Name $ServiceName } }
  'status' { Get-Service -Name $ServiceName }
}
