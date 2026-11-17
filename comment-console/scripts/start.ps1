param(
    [switch]$Demo,
    [switch]$ShowBrowser,
    [ValidateRange(0, 65535)][int]$Port = 0,
    [string]$Token = ''
)

$ErrorActionPreference = 'Stop'
$projectRoot = [System.IO.Path]::GetFullPath((Split-Path -Parent $PSScriptRoot))
$ocrLocalConfig = Join-Path $projectRoot 'backend/config/ocr.json'
if (Test-Path -LiteralPath $ocrLocalConfig -PathType Leaf) {
    $ocrConfig = Get-Content -LiteralPath $ocrLocalConfig -Raw -Encoding UTF8 | ConvertFrom-Json
    if ([string]::IsNullOrWhiteSpace($env:OCR_ALLOW_FAKE_IP) -and $ocrConfig.allow_fake_ip -eq $true) {
        $env:OCR_ALLOW_FAKE_IP = 'true'
    }
}
if ($Port -eq 0) {
    if ($Demo) { $Port = 18060 } else { $Port = 18061 }
}
if ($Token -eq '') { $Token = $env:AUTH_TOKEN }
$baseURL = "http://127.0.0.1:$Port"
$consoleURL = "$baseURL/console/"
$modeName = '正式'
if ($Demo) { $modeName = '演示' }
$headers = @{}
if ($Token) { $headers.Authorization = 'Bearer ' + $Token }

function Get-ExistingConsole {
    try {
        return Invoke-RestMethod -Uri "$baseURL/api/console/state" -Headers $headers -TimeoutSec 2
    } catch {
        return $null
    }
}

function Ensure-LocalOCR {
    if ($Demo -or ([string]$env:OCR_ENABLED).Trim() -eq 'false') { return }
    $ocrPort = 18063
    if (-not [string]::IsNullOrWhiteSpace($env:OCR_URL)) {
        $ocrAddress = $null
        if (-not [Uri]::TryCreate($env:OCR_URL, [UriKind]::Absolute, [ref]$ocrAddress) -or
            $ocrAddress.Scheme -ne 'http' -or
            $ocrAddress.Host -notin @('127.0.0.1', 'localhost') -or
            $ocrAddress.AbsolutePath -ne '/' -or $ocrAddress.Query -ne '' -or
            $ocrAddress.UserInfo -ne '' -or $ocrAddress.Fragment -ne '') {
            Write-Host '已配置自定义 OCR 服务地址，跳过本地自动启动；请自行确认服务可用。'
            return
        }
        $ocrPort = $ocrAddress.Port
    }
    try {
        & (Join-Path $PSScriptRoot 'start-ocr.ps1') -Port $ocrPort
    } catch {
        Write-Warning ("本地 OCR 暂不可用，应用仍可启动。原因：" + $_.Exception.Message)
    }
}

$existing = Get-ExistingConsole
if ($null -ne $existing -and $null -ne $existing.demo) {
    if ([bool]$existing.demo -ne [bool]$Demo) {
        throw "端口 $Port 上已运行另一模式的服务，请指定其他 -Port。"
    }
    Ensure-LocalOCR
    Write-Host "$modeName 服务已经运行：$consoleURL"
    if ($ShowBrowser -and -not $Demo) {
        Write-Host '浏览器显示模式只在启动时生效；如果已有服务使用无头模式，请先停止旧服务，再用 -ShowBrowser 启动。'
    }
    return
}
$portProbe = [System.Net.Sockets.TcpClient]::new()
try {
    $connection = $portProbe.ConnectAsync('127.0.0.1', $Port)
    try { [void]$connection.Wait(500) } catch {}
    if ($portProbe.Connected) {
        throw "端口 $Port 已占用，无法确认服务模式。请检查 -Token / AUTH_TOKEN，或指定其他 -Port。"
    }
} finally {
    $portProbe.Dispose()
}

$binary = Join-Path $projectRoot 'bin/comment-console.exe'
$webDirectory = Join-Path $projectRoot 'frontend/dist'
$configPath = Join-Path $projectRoot 'backend/config/mysql.json'
$dataDirectory = Join-Path $projectRoot 'backend/data'
if (-not (Test-Path -LiteralPath $binary -PathType Leaf) -or
    -not (Test-Path -LiteralPath (Join-Path $webDirectory 'index.html') -PathType Leaf)) {
    throw '缺少前端产物或后端程序，请先执行 scripts/build.ps1。'
}
$selectedDSN = $env:MYSQL_DSN
if ($Demo) { $selectedDSN = $env:MYSQL_DEMO_DSN }
if (-not $selectedDSN -and -not (Test-Path -LiteralPath $configPath -PathType Leaf)) {
    throw '请先从 backend/config/mysql.example.json 创建 mysql.json 并填写连接信息，或配置对应模式的 MySQL 环境变量。'
}
New-Item -ItemType Directory -Force $dataDirectory | Out-Null
Ensure-LocalOCR
$arguments = @(
    ('-headless=' + (-not [bool]$ShowBrowser).ToString().ToLowerInvariant()),
    "-port=127.0.0.1:$Port",
    ('-demo=' + ([bool]$Demo).ToString().ToLowerInvariant()),
    ('-web-dir="' + $webDirectory + '"'),
    ('-config="' + $configPath + '"')
)
$modeSlug = 'live'
if ($Demo) { $modeSlug = 'demo' }
$outputLog = Join-Path $dataDirectory "$modeSlug.stdout.log"
$errorLog = Join-Path $dataDirectory "$modeSlug.stderr.log"
$previousCookies, $previousToken = $env:COOKIES_PATH, $env:AUTH_TOKEN
try {
    $env:COOKIES_PATH = Join-Path $dataDirectory 'cookies.json'
    if ($Token) { $env:AUTH_TOKEN = $Token }
    $startOptions = @{
        FilePath = $binary
        ArgumentList = $arguments
        WorkingDirectory = $projectRoot
        WindowStyle = 'Hidden'
        RedirectStandardOutput = $outputLog
        RedirectStandardError = $errorLog
        PassThru = $true
    }
    $serverProcess = Start-Process @startOptions
} finally {
    $env:COOKIES_PATH, $env:AUTH_TOKEN = $previousCookies, $previousToken
}

for ($attempt = 0; $attempt -lt 30; $attempt++) {
    if ($serverProcess.HasExited) {
        throw "服务启动失败，请查看 $errorLog"
    }
    $ready = Get-ExistingConsole
    if ($null -ne $ready -and $null -ne $ready.demo -and [bool]$ready.demo -eq [bool]$Demo) {
        Write-Host "$modeName 服务已就绪：$consoleURL"
        return
    }
    Start-Sleep -Seconds 1
}
Write-Host "服务进程已启动，仍在初始化（首次正式启动可能下载浏览器）：$consoleURL"
Write-Host "运行日志：$errorLog"
