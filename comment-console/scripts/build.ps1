param([switch]$SkipTests)

$ErrorActionPreference = 'Stop'
$projectRoot = [System.IO.Path]::GetFullPath((Split-Path -Parent $PSScriptRoot))
$backend = Join-Path $projectRoot 'backend'
$frontend = Join-Path $projectRoot 'frontend'
$binaryDirectory = Join-Path $projectRoot 'bin'

Push-Location $frontend
try {
    & npm.cmd ci
    if ($LASTEXITCODE -ne 0) { throw '安装前端依赖失败。' }
    & npm.cmd run build
    if ($LASTEXITCODE -ne 0) { throw '构建前端失败。' }
} finally {
    Pop-Location
}

Push-Location $backend
try {
    if (-not $SkipTests) {
        & go test ./...
        if ($LASTEXITCODE -ne 0) { throw '后端测试失败。' }
    }
    New-Item -ItemType Directory -Force $binaryDirectory | Out-Null
    $previousGOOS, $previousGOARCH, $previousCGO = $env:GOOS, $env:GOARCH, $env:CGO_ENABLED
    try {
        $env:GOOS = 'windows'
        $env:GOARCH = 'amd64'
        $env:CGO_ENABLED = '0'
        & go build -trimpath -ldflags '-s -w -X xhs-comment-console/internal/server.version=v1.0.0' -o (Join-Path $binaryDirectory 'comment-console.exe') ./cmd/server
        if ($LASTEXITCODE -ne 0) { throw '构建后端失败。' }
    } finally {
        $env:GOOS, $env:GOARCH, $env:CGO_ENABLED = $previousGOOS, $previousGOARCH, $previousCGO
    }
} finally {
    Pop-Location
}
Write-Host "前端产物：$(Join-Path $frontend 'dist')"
Write-Host "后端程序：$(Join-Path $binaryDirectory 'comment-console.exe')"
