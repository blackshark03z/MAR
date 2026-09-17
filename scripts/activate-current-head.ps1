param(
    [string]$RepoRoot = (Split-Path -Parent $PSScriptRoot),
    [string]$ReleaseVersion = "",
    [switch]$OpenBrowser
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

function Get-MARRuntimeIdentity {
    try {
        return Invoke-RestMethod -Uri 'http://127.0.0.1:8787/api/runtime' -TimeoutSec 2
    }
    catch {
        return $null
    }
}

function Get-MARListener {
    return Get-NetTCPConnection -LocalAddress 127.0.0.1 -LocalPort 8787 -State Listen -ErrorAction SilentlyContinue | Select-Object -First 1
}

function Stop-MARListener {
    $listener = Get-MARListener
    if (-not $listener) {
        return
    }
    Stop-Process -Id $listener.OwningProcess -Force -ErrorAction Stop
    for ($i = 0; $i -lt 40; $i++) {
        if (-not (Get-MARListener)) {
            return
        }
        Start-Sleep -Milliseconds 250
    }
    throw "MAR listener on 127.0.0.1:8787 did not stop."
}

function Wait-MARRuntime {
    param(
        [Parameter(Mandatory = $true)][string]$ExpectedRevision,
        [Parameter(Mandatory = $true)][bool]$RequireTrusted
    )
    for ($i = 0; $i -lt 60; $i++) {
        $identity = Get-MARRuntimeIdentity
        if ($identity) {
            $revisionMatches = [string]$identity.source_revision -eq $ExpectedRevision
            $trustMatches = (-not $RequireTrusted) -or ([bool]$identity.trusted_for_release)
            if ($revisionMatches -and $trustMatches) {
                return $identity
            }
        }
        Start-Sleep -Milliseconds 500
    }
    $last = Get-MARRuntimeIdentity
    throw "MAR runtime verification failed. Expected source_revision=$ExpectedRevision and trusted_for_release=$RequireTrusted. Last identity: $($last | ConvertTo-Json -Compress -Depth 8)"
}

function Copy-BackupFile {
    param(
        [Parameter(Mandatory = $true)][string]$Source,
        [Parameter(Mandatory = $true)][string]$Destination
    )
    if (Test-Path -LiteralPath $Source -PathType Leaf) {
        $parent = Split-Path -Parent $Destination
        New-Item -ItemType Directory -Force -Path $parent | Out-Null
        Copy-Item -LiteralPath $Source -Destination $Destination -Force
    }
}

function Restore-BackupFile {
    param(
        [Parameter(Mandatory = $true)][string]$Backup,
        [Parameter(Mandatory = $true)][string]$Destination
    )
    if (Test-Path -LiteralPath $Backup -PathType Leaf) {
        $parent = Split-Path -Parent $Destination
        New-Item -ItemType Directory -Force -Path $parent | Out-Null
        Copy-Item -LiteralPath $Backup -Destination $Destination -Force
    }
    elseif (Test-Path -LiteralPath $Destination -PathType Leaf) {
        Remove-Item -LiteralPath $Destination -Force
    }
}

$RepoRoot = (Resolve-Path -LiteralPath $RepoRoot).Path
$dataRoot = Join-Path $RepoRoot '.mar'
$runtimeRoot = Join-Path $dataRoot 'runtime'
$stableExe = Join-Path $runtimeRoot 'mar-v1-stable.exe'
$releaseManifest = Join-Path $runtimeRoot 'release-manifest.json'
$db = Join-Path $dataRoot 'mar.db'
$dbWal = "$db-wal"
$dbShm = "$db-shm"
$go = Join-Path $runtimeRoot 'go-portable\go\bin\go.exe'
$launcher = Join-Path $RepoRoot 'scripts\start-owner-console.ps1'

foreach ($required in @($go, $launcher)) {
    if (-not (Test-Path -LiteralPath $required -PathType Leaf)) {
        throw "Required activation prerequisite is missing: $required"
    }
}

$head = (& git -C $RepoRoot rev-parse HEAD).Trim()
if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($head)) {
    throw 'Unable to resolve current Git HEAD.'
}
$dirty = @(& git -C $RepoRoot status --porcelain)
if ($LASTEXITCODE -ne 0) {
    throw 'Unable to inspect Git working tree.'
}
if ($dirty.Count -ne 0) {
    throw 'Activation requires a clean Git working tree.'
}

$shortHead = $head.Substring(0, [Math]::Min(12, $head.Length))
if ([string]::IsNullOrWhiteSpace($ReleaseVersion)) {
    $ReleaseVersion = "local-$shortHead"
}

$buildTimestamp = [DateTime]::UtcNow.ToString('yyyy-MM-ddTHH:mm:ssZ')
$stagingRoot = Join-Path $runtimeRoot 'staging'
New-Item -ItemType Directory -Force -Path $stagingRoot | Out-Null
$candidateExe = Join-Path $stagingRoot "mar-$shortHead.exe"
$candidateManifest = Join-Path $stagingRoot "release-manifest-$shortHead.json"
$ldflags = "-X main.embeddedReleaseVersion=$ReleaseVersion -X main.embeddedBuildTimestamp=$buildTimestamp"

Push-Location $RepoRoot
try {
    & $go build -trimpath -ldflags $ldflags -o $candidateExe '.\cmd\mar'
    if ($LASTEXITCODE -ne 0) {
        throw "MAR build failed with exit code $LASTEXITCODE."
    }
}
finally {
    Pop-Location
}

& $candidateExe release-manifest -data-root $dataRoot -release $ReleaseVersion -out $candidateManifest
if ($LASTEXITCODE -ne 0) {
    throw "MAR release-manifest generation failed with exit code $LASTEXITCODE."
}

$priorIdentity = Get-MARRuntimeIdentity
$priorListener = Get-MARListener
if ($priorListener -and -not $priorIdentity) {
    throw "Port 127.0.0.1:8787 is occupied but does not expose a valid MAR runtime identity."
}
$priorWasRunning = [bool]$priorListener
$priorRevision = if ($priorIdentity) { [string]$priorIdentity.source_revision } else { "" }

$stamp = [DateTime]::UtcNow.ToString('yyyyMMdd-HHmmss')
$backupRoot = Join-Path $dataRoot "recovery\activation-$stamp-$shortHead"
$backupRuntime = Join-Path $backupRoot 'runtime'
$backupData = Join-Path $backupRoot 'data'
New-Item -ItemType Directory -Force -Path $backupRuntime, $backupData | Out-Null

$metadata = [ordered]@{
    created_at_utc = [DateTime]::UtcNow.ToString('o')
    target_revision = $head
    target_release = $ReleaseVersion
    previous_running = $priorWasRunning
    previous_identity = $priorIdentity
}
$metadata | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath (Join-Path $backupRoot 'activation.json') -Encoding UTF8

try {
    if ($priorWasRunning) {
        Stop-MARListener
    }

    Copy-BackupFile -Source $stableExe -Destination (Join-Path $backupRuntime 'mar-v1-stable.exe')
    Copy-BackupFile -Source $releaseManifest -Destination (Join-Path $backupRuntime 'release-manifest.json')
    Copy-BackupFile -Source $db -Destination (Join-Path $backupData 'mar.db')
    Copy-BackupFile -Source $dbWal -Destination (Join-Path $backupData 'mar.db-wal')
    Copy-BackupFile -Source $dbShm -Destination (Join-Path $backupData 'mar.db-shm')

    Copy-Item -LiteralPath $candidateExe -Destination $stableExe -Force
    Copy-Item -LiteralPath $candidateManifest -Destination $releaseManifest -Force

    & $launcher -RepoRoot $RepoRoot
    $identity = Wait-MARRuntime -ExpectedRevision $head -RequireTrusted $true

    Write-Host "MAR ACTIVATION PASS"
    Write-Host "HEAD: $head"
    Write-Host "Release: $ReleaseVersion"
    Write-Host "Backup: $backupRoot"
    Write-Host "Runtime: $($identity | ConvertTo-Json -Compress -Depth 8)"

    if ($OpenBrowser) {
        Start-Process 'http://127.0.0.1:8787/' | Out-Null
    }
}
catch {
    $activationError = $_
    try {
        Stop-MARListener

        Restore-BackupFile -Backup (Join-Path $backupRuntime 'mar-v1-stable.exe') -Destination $stableExe
        Restore-BackupFile -Backup (Join-Path $backupRuntime 'release-manifest.json') -Destination $releaseManifest
        Restore-BackupFile -Backup (Join-Path $backupData 'mar.db') -Destination $db
        Restore-BackupFile -Backup (Join-Path $backupData 'mar.db-wal') -Destination $dbWal
        Restore-BackupFile -Backup (Join-Path $backupData 'mar.db-shm') -Destination $dbShm

        if ($priorWasRunning) {
            & $launcher -RepoRoot $RepoRoot
            if (-not [string]::IsNullOrWhiteSpace($priorRevision)) {
                [void](Wait-MARRuntime -ExpectedRevision $priorRevision -RequireTrusted $false)
            }
        }
    }
    catch {
        throw "Activation failed and rollback also failed. Activation error: $($activationError.Exception.Message) Rollback error: $($_.Exception.Message) Backup: $backupRoot"
    }
    throw "Activation failed; previous runtime state was restored. $($activationError.Exception.Message) Backup: $backupRoot"
}
