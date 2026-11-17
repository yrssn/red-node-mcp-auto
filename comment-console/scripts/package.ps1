param()

$ErrorActionPreference = 'Stop'
$projectRoot = [System.IO.Path]::GetFullPath((Split-Path -Parent $PSScriptRoot))
$archiveRoot = 'comment-console-v1.0.0'
$releaseDirectory = Join-Path $projectRoot 'release'
$archivePath = Join-Path $releaseDirectory "$archiveRoot.zip"
$selectedFiles = @{}
$requiredFiles = @(
    'README.md',
    'LICENSE',
    '.dockerignore',
    '.gitattributes',
    'backend/README.md',
    'backend/.run/Comment Console (amd64).run.xml',
    'backend/go.mod',
    'backend/go.sum',
    'backend/cmd/server/main.go',
    'backend/internal/browser/browser_version.txt',
    'backend/config/mysql.example.json',
    'backend/config/mysql.docker.example.json',
    'backend/config/ocr.example.json',
    'frontend/package.json',
    'frontend/README.md',
    'frontend/package-lock.json',
    'frontend/index.html',
    'frontend/vite.config.js',
    'frontend/dist/index.html',
    'deploy/Dockerfile',
    'deploy/compose.yaml',
    'deploy/compose.external.yaml',
    'deploy/.env.example',
    'deploy/console-entrypoint.sh',
    'deploy/mysql-init.sql',
    'backend/schema/schema.sql',
    'scripts/build.ps1',
    'scripts/start.ps1',
    'scripts/start-ocr.ps1',
    'scripts/install-ocr.ps1',
    'ocr/server.py',
    'ocr/test_server.py',
    'ocr/requirements.txt',
    'ocr/Dockerfile',
    'ocr/README.md',
    'scripts/seed-programming.ps1',
    'presets/programming.json',
    'scripts/package.ps1'
)

function Add-PackageFile([System.IO.FileInfo]$file) {
    $fullPath = [System.IO.Path]::GetFullPath($file.FullName)
    $prefix = $projectRoot.TrimEnd('\', '/') + [System.IO.Path]::DirectorySeparatorChar
    if (-not $fullPath.StartsWith($prefix, [StringComparison]::OrdinalIgnoreCase)) {
        throw '只允许打包当前独立项目中的文件。'
    }
    if (($file.Attributes -band [System.IO.FileAttributes]::ReparsePoint) -ne 0) {
        throw "不打包符号链接：$($file.Name)"
    }
    for ($parentDirectory = $file.Directory; $null -ne $parentDirectory -and $parentDirectory.FullName -ne $projectRoot; $parentDirectory = $parentDirectory.Parent) {
        if (($parentDirectory.Attributes -band [System.IO.FileAttributes]::ReparsePoint) -ne 0) {
            throw "不打包符号链接目录：$($parentDirectory.Name)"
        }
    }
    $relative = $fullPath.Substring($prefix.Length).Replace('\', '/')
    if ($relative -match '(^|/)(\.git|\.codex|\.agents|\.runtime|__pycache__|node_modules|data|release)(/|$)' -or
        $file.Name -eq '.env' -or $file.Name -eq 'cookies.json' -or
        ($relative -like 'backend/config/*.json' -and $file.Name -notlike '*.example.json')) {
        throw "白名单意外包含本地配置或数据：$relative"
    }
    $selectedFiles[$relative] = $file
}

foreach ($relative in $requiredFiles) {
    $filePath = Join-Path $projectRoot $relative
    if (-not (Test-Path -LiteralPath $filePath -PathType Leaf)) {
        throw "缺少交付文件：$relative。请先完成整理和前端构建。"
    }
    Add-PackageFile (Get-Item -LiteralPath $filePath -Force)
}
foreach ($directory in @('backend/cmd', 'backend/internal')) {
    Get-ChildItem -LiteralPath (Join-Path $projectRoot $directory) -Recurse -File -Filter '*.go' | ForEach-Object {
        Add-PackageFile $_
    }
}
foreach ($directory in @('backend/schema')) {
    $directoryPath = Join-Path $projectRoot $directory
    if (Test-Path -LiteralPath $directoryPath -PathType Container) {
        Get-ChildItem -LiteralPath $directoryPath -Recurse -File -Filter '*.sql' | ForEach-Object {
            Add-PackageFile $_
        }
    }
}
Get-ChildItem -LiteralPath (Join-Path $projectRoot 'docs/licenses') -File | ForEach-Object {
    Add-PackageFile $_
}
$contentDirectories = @{
    'frontend/src' = @('.js', '.vue', '.css', '.json', '.svg', '.png', '.jpg', '.jpeg', '.webp', '.woff', '.woff2', '.ttf')
    'frontend/dist' = @('.html', '.js', '.css', '.json', '.svg', '.png', '.jpg', '.jpeg', '.webp', '.gif', '.ico', '.woff', '.woff2', '.ttf', '.txt')
    'docs' = @('.md', '.png', '.jpg', '.svg')
    'licenses' = @('', '.md', '.txt')
}
foreach ($directory in $contentDirectories.Keys) {
    $directoryPath = Join-Path $projectRoot $directory
    if (-not (Test-Path -LiteralPath $directoryPath -PathType Container)) { continue }
    $extensions = $contentDirectories[$directory]
    Get-ChildItem -LiteralPath $directoryPath -Recurse -File | Where-Object {
        $_.Extension.ToLowerInvariant() -in $extensions
    } | ForEach-Object {
        Add-PackageFile $_
    }
}
foreach ($optional in @('.gitignore', 'bin/comment-console.exe')) {
    $optionalPath = Join-Path $projectRoot $optional
    if (Test-Path -LiteralPath $optionalPath -PathType Leaf) {
        Add-PackageFile (Get-Item -LiteralPath $optionalPath -Force)
    }
}

# 仅打包本项目的白名单文件，不读取父仓库、账号配置或运行目录。
Add-Type -AssemblyName System.IO.Compression
Add-Type -AssemblyName System.IO.Compression.FileSystem
$zipTime = [DateTimeOffset]::new(1980, 1, 1, 0, 0, 0, [TimeSpan]::Zero)
New-Item -ItemType Directory -Force $releaseDirectory | Out-Null
$stream = [System.IO.File]::Open($archivePath, [System.IO.FileMode]::Create, [System.IO.FileAccess]::Write)
$zip = $null
try {
    $zip = [System.IO.Compression.ZipArchive]::new($stream, [System.IO.Compression.ZipArchiveMode]::Create, $false, [System.Text.UTF8Encoding]::new($false))
    $directories = @{}
    $directories["$archiveRoot/"] = $true
    foreach ($relative in $selectedFiles.Keys) {
        $parts = $relative.Split('/')
        $parent = "$archiveRoot/"
        for ($index = 0; $index -lt $parts.Length - 1; $index++) {
            $parent += $parts[$index] + '/'
            $directories[$parent] = $true
        }
    }
    foreach ($directory in @($directories.Keys | Sort-Object)) {
        $entry = $zip.CreateEntry($directory)
        $entry.LastWriteTime = $zipTime
    }
    foreach ($relative in @($selectedFiles.Keys | Sort-Object)) {
        $entry = $zip.CreateEntry("$archiveRoot/$relative", [System.IO.Compression.CompressionLevel]::Optimal)
        $entry.LastWriteTime = $zipTime
        $inputStream = [System.IO.File]::OpenRead($selectedFiles[$relative].FullName)
        $entryStream = $entry.Open()
        try {
            $inputStream.CopyTo($entryStream)
        } finally {
            $entryStream.Dispose()
            $inputStream.Dispose()
        }
    }
} finally {
    if ($null -ne $zip) { $zip.Dispose() }
    $stream.Dispose()
}
$hash = (Get-FileHash -LiteralPath $archivePath -Algorithm SHA256).Hash.ToLowerInvariant()
Set-Content -LiteralPath ($archivePath + '.sha256') -Value "$hash  $(Split-Path -Leaf $archivePath)" -Encoding ascii
Write-Host "独立项目交付包：$archivePath"
Write-Host "文件数量：$($selectedFiles.Count)，SHA256：$hash"
