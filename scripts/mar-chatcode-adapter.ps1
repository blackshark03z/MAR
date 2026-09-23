param(
    [ValidateSet("project", "action", "submit", "task", "control", "brain_turn", "brain_respond")]
    [string]$Tool,

    [string]$ArgumentsJson = "{}",

    [switch]$ListTools,

    [string]$OwnerRuntimeUrl = "http://127.0.0.1:8787/api/runtime",

    [ValidateRange(1, 60)]
    [int]$TimeoutSec = 10
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$CanonicalTools = @(
    "action",
    "action",
    "brain_respond",
    "brain_turn",
    "control",
    "project",
    "submit",
    "task"
)


$OptionalTools = @(
    "brain_turn_fast"
)
function Write-AdapterFailure {
    param(
        [string]$Code,
        [string]$Message
    )

    [pscustomobject]@{
        ok      = $false
        code    = $Code
        message = $Message
    } | ConvertTo-Json -Depth 8 -Compress | Write-Output
}

function Test-LoopbackUri {
    param([uri]$Uri)

    if ($Uri.Scheme -ne "http") {
        return $false
    }

    if ($Uri.Host -eq "localhost") {
        return $true
    }

    $parsed = $null
    if ([System.Net.IPAddress]::TryParse($Uri.Host, [ref]$parsed)) {
        return [System.Net.IPAddress]::IsLoopback($parsed)
    }

    return $false
}

try {
    if (-not $ListTools -and [string]::IsNullOrWhiteSpace($Tool)) {
        Write-AdapterFailure -Code "TOOL_REQUIRED" -Message "Specify -Tool or use -ListTools."
        exit 2
    }

    if (-not $ListTools -and $CanonicalTools -notcontains $Tool) {
        Write-AdapterFailure -Code "TOOL_NOT_ALLOWED" -Message "Only canonical MAR tools are allowed."
        exit 2
    }

    $runtime = Invoke-RestMethod -Uri $OwnerRuntimeUrl -Method Get -TimeoutSec $TimeoutSec
    $connection = @($runtime.connections | Where-Object { $_.id -eq "openai-tunnel" }) | Select-Object -First 1

    if ($null -eq $connection) {
        Write-AdapterFailure -Code "MCP_TARGET_UNAVAILABLE" -Message "MAR runtime did not expose the OpenAI tunnel local target."
        exit 3
    }

    $localTarget = [string]$connection.local_target
    if ([string]::IsNullOrWhiteSpace($localTarget)) {
        Write-AdapterFailure -Code "MCP_TARGET_UNAVAILABLE" -Message "MAR OpenAI tunnel has no local MCP target."
        exit 3
    }

    $mcpUri = [uri]$localTarget
    if (-not (Test-LoopbackUri -Uri $mcpUri)) {
        Write-AdapterFailure -Code "NON_LOOPBACK_TARGET_REJECTED" -Message "Adapter only calls the MAR loopback MCP target."
        exit 3
    }

    $headers = @{
        Accept = "application/json, text/event-stream"
        "Content-Type" = "application/json"
    }

    if ($ListTools) {
        $payload = @{
            jsonrpc = "2.0"
            id      = "chatcode-mar-tools"
            method  = "tools/list"
            params  = @{}
        }
    } else {
        try {
            $arguments = $ArgumentsJson | ConvertFrom-Json
        } catch {
            Write-AdapterFailure -Code "INVALID_ARGUMENTS_JSON" -Message $_.Exception.Message
            exit 2
        }

        $payload = @{
            jsonrpc = "2.0"
            id      = "chatcode-mar-call"
            method  = "tools/call"
            params  = @{
                name      = $Tool
                arguments = $arguments
            }
        }
    }

    $body = $payload | ConvertTo-Json -Depth 32 -Compress
    $wire = Invoke-WebRequest -UseBasicParsing -Uri $mcpUri.AbsoluteUri -Method Post -Headers $headers -Body $body -TimeoutSec $TimeoutSec
    $response = $wire.Content | ConvertFrom-Json

    $errorProperty = $response.PSObject.Properties["error"]
    if ($null -ne $errorProperty -and $null -ne $errorProperty.Value) {
        $errorObject = $errorProperty.Value
        $messageProperty = $errorObject.PSObject.Properties["message"]
        $message = if ($null -ne $messageProperty -and $null -ne $messageProperty.Value) { [string]$messageProperty.Value } else { "MAR MCP returned a JSON-RPC error." }
        Write-AdapterFailure -Code "JSONRPC_ERROR" -Message $message
        exit 4
    }

    $resultProperty = $response.PSObject.Properties["result"]
    if ($null -eq $resultProperty -or $null -eq $resultProperty.Value) {
        Write-AdapterFailure -Code "JSONRPC_RESULT_MISSING" -Message "MAR MCP response did not contain a result."
        exit 4
    }
    $result = $resultProperty.Value

    if ($ListTools) {
        $toolsProperty = $result.PSObject.Properties["tools"]
        if ($null -eq $toolsProperty -or $null -eq $toolsProperty.Value) {
            Write-AdapterFailure -Code "TOOLS_LIST_MISSING" -Message "MAR MCP tools/list response did not contain tools."
            exit 5
        }
        $names = @($toolsProperty.Value | ForEach-Object { [string]$_.name } | Sort-Object)
        $unexpected = @($names | Where-Object { $CanonicalTools -notcontains $_ -and $OptionalTools -notcontains $_ })
        $missing = @($CanonicalTools | Where-Object { $names -notcontains $_ })

        if ($unexpected.Count -gt 0 -or $missing.Count -gt 0) {
            Write-AdapterFailure -Code "TOOL_SURFACE_MISMATCH" -Message ("Expected canonical MAR surface. Missing={0}; Unexpected={1}" -f ($missing -join ","), ($unexpected -join ","))
            exit 5
        }

        [pscustomobject]@{
            ok    = $true
            tools = $names
        } | ConvertTo-Json -Depth 8 -Compress | Write-Output
        exit 0
    }

    $isError = $false
    if ($null -ne $result -and $null -ne $result.PSObject.Properties["isError"]) {
        $isError = [bool]$result.isError
    }

    if ($isError) {
        $toolMessage = "MAR MCP tool returned an application error."
        if ($null -ne $result.content) {
            $textItem = @($result.content | Where-Object { $_.type -eq "text" }) | Select-Object -First 1
            if ($null -ne $textItem -and -not [string]::IsNullOrWhiteSpace([string]$textItem.text)) {
                $toolMessage = [string]$textItem.text
            }
        }
        Write-AdapterFailure -Code "MAR_TOOL_ERROR" -Message $toolMessage
        exit 6
    }

    $structuredProperty = $result.PSObject.Properties["structuredContent"]
    $contentProperty = $result.PSObject.Properties["content"]
    [pscustomobject]@{
        ok                = $true
        tool              = $Tool
        structuredContent = if ($null -ne $structuredProperty) { $structuredProperty.Value } else { $null }
        content           = if ($null -ne $contentProperty) { $contentProperty.Value } else { $null }
    } | ConvertTo-Json -Depth 32 -Compress | Write-Output
    exit 0
} catch {
    Write-AdapterFailure -Code "ADAPTER_FAILURE" -Message $_.Exception.Message
    exit 10
}
