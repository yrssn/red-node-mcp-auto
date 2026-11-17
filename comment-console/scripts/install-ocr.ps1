param([string]$PythonPath = '')

$ErrorActionPreference = 'Stop'
$projectRoot = [System.IO.Path]::GetFullPath((Split-Path -Parent $PSScriptRoot))
$environmentDirectory = Join-Path $projectRoot '.runtime/ocr'
$environmentPython = Join-Path $environmentDirectory 'Scripts/python.exe'
$requirements = Join-Path $projectRoot 'ocr/requirements.txt'
if (-not (Test-Path -LiteralPath $requirements -PathType Leaf)) {
    throw '缺少 ocr/requirements.txt。'
}
if ($PythonPath -eq '') {
    $pythonCommand = Get-Command python -ErrorAction SilentlyContinue
    if ($null -eq $pythonCommand) { throw '请安装 Python 3.11–3.13 x64，或使用 -PythonPath 指定已有解释器。' }
    $PythonPath = $pythonCommand.Source
}
& $PythonPath -c "import struct,sys; assert (3,11) <= sys.version_info[:2] < (3,14) and struct.calcsize('P') == 8, 'Python 3.11-3.13 x64 is required'"
if ($LASTEXITCODE -ne 0) { throw 'OCR 需要 Python 3.11–3.13 x64。' }

# 依赖只装入项目私有虚拟环境，不改全局 Python。
if (-not (Test-Path -LiteralPath $environmentPython -PathType Leaf)) {
    & $PythonPath -m venv $environmentDirectory
    if ($LASTEXITCODE -ne 0) { throw '创建 OCR 虚拟环境失败。' }
}
& $environmentPython -m pip install -r $requirements
if ($LASTEXITCODE -ne 0) { throw '安装 OCR 固定依赖失败，请检查安装输出及网络。' }
Push-Location (Join-Path $projectRoot 'ocr')
try {
    & $environmentPython -c "from server import load_engine; load_engine(); print('OCR models ready (CPU)')"
    if ($LASTEXITCODE -ne 0) { throw 'OCR 本地模型验证失败，请检查固定依赖及随包权重。' }
} finally {
    Pop-Location
}
Write-Host 'OCR 已安装到项目 .runtime/ocr，未修改全局 Python。'
Write-Host '可执行 scripts/start-ocr.ps1 启动；后续启动不会重复安装依赖。'
