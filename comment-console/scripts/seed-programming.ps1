param(
    [string]$BaseUrl = 'http://127.0.0.1:18061',
    [string]$Token = ''
)

$ErrorActionPreference = 'Stop'
$projectRoot = [System.IO.Path]::GetFullPath((Split-Path -Parent $PSScriptRoot))
$presetPath = Join-Path $projectRoot 'presets/programming.json'
$preset = Get-Content -LiteralPath $presetPath -Raw -Encoding UTF8 | ConvertFrom-Json
$apiBase = $BaseUrl.TrimEnd('/') + '/api/console'
if ($Token -eq '') { $Token = $env:AUTH_TOKEN }
$headers = @{}
if ($Token) { $headers.Authorization = 'Bearer ' + $Token }

function Get-ConsoleState {
    Invoke-RestMethod -Uri "$apiBase/state" -Headers $headers -TimeoutSec 10
}

function Save-ConsoleResource([string]$kind, $value) {
    $jsonBytes = [Text.Encoding]::UTF8.GetBytes(($value | ConvertTo-Json -Depth 12 -Compress))
    $null = Invoke-RestMethod -Uri "$apiBase/$kind" -Method Post -Headers $headers -ContentType 'application/json; charset=utf-8' -Body $jsonBytes -TimeoutSec 10
}

# 同名项保留现有内容，重复导入只补齐缺失项。
$state = Get-ConsoleState
Save-ConsoleResource 'settings' @{ send_paused = $true; send_interval = $state.settings.send_interval }
$added = @{ strategies = 0; materials = 0; tasks = 0 }
foreach ($kind in @('strategies', 'materials')) {
    foreach ($item in $preset.$kind) {
        if (@($state.$kind | Where-Object { $_.name -eq $item.name }).Count -gt 0) { continue }
        Save-ConsoleResource $kind $item
        $added[$kind]++
    }
}
$state = Get-ConsoleState
$exclude = '论文代写,代写论文,毕业设计代做,毕设代做,课程作业代做,代考,代答辩,代提交,包过,保过,刷课,刷分,学术造假,枪手,接单推广,招生广告,成品源码售卖'
foreach ($item in $preset.tasks) {
    if (@($state.tasks | Where-Object { $_.name -eq $item.name }).Count -gt 0) { continue }
    $strategy = @($state.strategies | Where-Object { $_.name -eq $item.strategy })
    if ($strategy.Count -ne 1) { throw "评论策略名称不唯一或不存在：$($item.strategy)" }
    $task = @{
        name = $item.name
        sources = $item.sources
        model_id = ''
        analysis_mode = 'model'
        include_closed = $false
        match_keywords = $item.keywords
        demand_keywords = '求助,急求,帮忙,帮我,找人,有没有人,有谁会,谁能,代做,开发需求,定制开发,报错,怎么办,怎么解决,不会做'
        strategy_id = $strategy[0].id
        goal = "寻找$($item.topic)。优先原帖明确自述大学生/计算机相关背景且主动提出具体问题或合法开发需求的作者；不要从外貌或昵称推测学历年龄。排除服务商营销、无求助意图的经验分享、已结束需求、重复转载，以及论文代写、代考、冒名提交课程/毕业考核等作弊服务。意图不明时判为不匹配，不为触达而生成无关评论。"
        exclude = $exclude
        publish_time = '一周内'
        note_type = '不限'
        interval = 3600
        limit = 5
        send_limit = 1
        auto_send = $false
        scheduled = $false
        paused = $true
        collect_paused = $false
        analyze_paused = $false
        send_paused = $true
    }
    Save-ConsoleResource 'tasks' $task
    $added.tasks++
}
$finalState = Get-ConsoleState
[pscustomobject]@{
    preset = $preset.name
    added_strategies = $added.strategies
    added_materials = $added.materials
    added_tasks = $added.tasks
    send_paused = $finalState.settings.send_paused
    next_step = '刷新页面；配置并测试真实模型后，编辑任务选择模型。预置任务仍需手动运行、逐条审核。'
} | ConvertTo-Json -Depth 3
