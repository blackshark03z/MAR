param(
    [string]$RepoRoot = (Split-Path -Parent $PSScriptRoot),
    [switch]$OpenBrowser
)

$ErrorActionPreference = 'Stop'

function Test-MAROwnerConsole {
    try {
        $response = Invoke-WebRequest -UseBasicParsing -Uri 'http://127.0.0.1:8787/' -TimeoutSec 2
        return $response.StatusCode -eq 200 -and $response.Content -match '<title>MAR Console</title>'
    }
    catch {
        return $false
    }
}

$RepoRoot = (Resolve-Path -LiteralPath $RepoRoot).Path
$dataRoot = Join-Path $RepoRoot '.mar'
$runtimeRoot = Join-Path $dataRoot 'runtime'
$exe = Join-Path $runtimeRoot 'mar-v1-stable.exe'
$db = Join-Path $dataRoot 'mar.db'
$go = Join-Path $runtimeRoot 'go-portable\go\bin\go.exe'
$stdoutLog = Join-Path $runtimeRoot 'owner-console.stdout.log'
$stderrLog = Join-Path $runtimeRoot 'owner-console.stderr.log'

if (Test-MAROwnerConsole) {
    if ($OpenBrowser) {
        Start-Process 'http://127.0.0.1:8787/' | Out-Null
    }
    exit 0
}

$listener = Get-NetTCPConnection -LocalAddress 127.0.0.1 -LocalPort 8787 -State Listen -ErrorAction SilentlyContinue
if ($listener) {
    throw "Port 127.0.0.1:8787 is already owned by PID $($listener.OwningProcess), but it is not a valid MAR Console."
}

foreach ($required in @($exe, $db, $go)) {
    if (-not (Test-Path -LiteralPath $required -PathType Leaf)) {
        throw "Required MAR runtime file is missing: $required"
    }
}

New-Item -ItemType Directory -Force -Path $runtimeRoot | Out-Null
$arguments = @(
    'ui',
    '-db', ('"{0}"' -f $db),
    '-data-root', ('"{0}"' -f $dataRoot),
    '-listen', '127.0.0.1:8787',
    '-brain', 'web',
    '-go', ('"{0}"' -f $go),
    '-max-workers', '2'
)

$process = Start-Process -FilePath $exe -ArgumentList $arguments -WorkingDirectory $RepoRoot -WindowStyle Hidden -RedirectStandardOutput $stdoutLog -RedirectStandardError $stderrLog -PassThru

for ($attempt = 0; $attempt -lt 40; $attempt++) {
    if (Test-MAROwnerConsole) {
        if ($OpenBrowser) {
            Start-Process 'http://127.0.0.1:8787/' | Out-Null
        }
        exit 0
    }
    if ($process.HasExited) {
        $tail = ''
        if (Test-Path -LiteralPath $stderrLog) {
            $tail = ((Get-Content -LiteralPath $stderrLog -Tail 20 -ErrorAction SilentlyContinue) -join [Environment]::NewLine)
        }
        throw "MAR Console exited before becoming ready (exit $($process.ExitCode)). $tail"
    }
    Start-Sleep -Milliseconds 250
}

throw 'MAR Console did not become ready at http://127.0.0.1:8787 within 10 seconds.'
