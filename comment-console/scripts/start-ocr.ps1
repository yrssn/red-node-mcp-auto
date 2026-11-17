param([ValidateRange(1, 65535)][int]$Port = 18063)

$ErrorActionPreference = 'Stop'
$projectRoot = [System.IO.Path]::GetFullPath((Split-Path -Parent $PSScriptRoot))
$healthURL = "http://127.0.0.1:$Port/health"
$python = Join-Path $projectRoot '.runtime/ocr/Scripts/python.exe'
$server = Join-Path $projectRoot 'ocr/server.py'
$dataDirectory = Join-Path $projectRoot 'backend/data'

function Get-KnownOCRService {
    try {
        $health = Invoke-RestMethod -Uri $healthURL -TimeoutSec 2
        if ($health.ok -eq $true -and [string]$health.engine -match 'RapidOCR') { return $health }
    } catch {}
    return $null
}

if ($null -ne (Get-KnownOCRService)) {
    Write-Host "本地 OCR 已就绪：127.0.0.1:$Port"
    return
}
$probe = [System.Net.Sockets.TcpClient]::new()
try {
    $connection = $probe.ConnectAsync('127.0.0.1', $Port)
    try { [void]$connection.Wait(500) } catch {}
    if ($probe.Connected) {
        throw "端口 $Port 已被占用，且无法确认是 RapidOCR；请检查或指定其他端口。不会停止已有进程。"
    }
} finally {
    $probe.Dispose()
}
if (-not (Test-Path -LiteralPath $python -PathType Leaf)) {
    throw 'OCR 尚未安装，请先执行 scripts/install-ocr.ps1。'
}
if (-not (Test-Path -LiteralPath $server -PathType Leaf)) {
    throw '缺少 ocr/server.py。'
}
New-Item -ItemType Directory -Force $dataDirectory | Out-Null
$outputLog = Join-Path $dataDirectory 'ocr.stdout.log'
$errorLog = Join-Path $dataDirectory 'ocr.stderr.log'
$previousBind, $previousPort, $previousBytecode = $env:BIND, $env:PORT, $env:PYTHONDONTWRITEBYTECODE
try {
    $env:BIND = '127.0.0.1'
    $env:PORT = [string]$Port
    $env:PYTHONDONTWRITEBYTECODE = '1'
    $startOptions = @{
        FilePath = $python
        ArgumentList = @('-u', ('"' + $server + '"'))
        WorkingDirectory = $projectRoot
        WindowStyle = 'Hidden'
        RedirectStandardOutput = $outputLog
        RedirectStandardError = $errorLog
        PassThru = $true
    }
    $process = Start-Process @startOptions
} finally {
    $env:BIND, $env:PORT, $env:PYTHONDONTWRITEBYTECODE = $previousBind, $previousPort, $previousBytecode
}
for ($attempt = 0; $attempt -lt 90; $attempt++) {
    if ($process.HasExited) { throw "OCR 启动失败，请查看 $errorLog" }
    if ($null -ne (Get-KnownOCRService)) {
        Write-Host "本地 OCR 已就绪：127.0.0.1:$Port"
        return
    }
    Start-Sleep -Seconds 1
}
throw "OCR 进程仍在初始化，请检查 $errorLog；应用可继续使用其他功能。"
