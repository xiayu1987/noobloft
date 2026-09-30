# Copyright (c) 2026 xiayu
# Contact: 126240622+xiayu1987@users.noreply.github.com
# SPDX-License-Identifier: MIT

param([Parameter(ValueFromRemainingArguments = $true)][string[]]$CliArgs = @())
$ErrorActionPreference = 'Stop'

function Get-SwarmLanguage {
    foreach ($name in @('NOOBLOFT_LANG', 'LC_ALL', 'LC_MESSAGES', 'LANG')) {
        $value = [Environment]::GetEnvironmentVariable($name)
        if ($value) { if ($value.Trim().ToLower() -match '^en([-_.]|$)') { return 'en' } else { return 'zh-CN' } }
    }
    return 'zh-CN'
}
$Lang = Get-SwarmLanguage
function T([string]$zh, [string]$en) { if ($Lang -eq 'en') { $en } else { $zh } }

$Dir = Join-Path $HOME '.noobloft'
$Mode = 'private'
$modeSpecified = $false
$InitArgs = @()
$Help = $false
$i = 0
while ($i -lt $CliArgs.Count) {
    $arg = $CliArgs[$i]
    $needValue = { param($name) if ($i + 1 -ge $CliArgs.Count) { throw (T "$name 需要一个值" "$name requires a value") } ; $CliArgs[$i + 1] }
    if ($arg -match '^--?dir$') { $Dir = & $needValue $arg; $i += 2 }
    elseif ($arg -match '^--?mode$') { $Mode = & $needValue $arg; $modeSpecified = $true; $i += 2 }
    elseif ($arg -match '^--?(h|help|\?)$') { $Help = $true; $i += 1 }
    elseif ($arg -eq '--') { if ($i + 1 -lt $CliArgs.Count) { $InitArgs = $CliArgs[($i + 1)..($CliArgs.Count - 1)] } ; break }
    else { throw (T "未知参数: $arg（init 参数请放在 -- 之后）" "Unknown argument: $arg (put init arguments after --)") }
}
if ($Mode -notin @('private', 'shared')) { throw (T '--mode 必须为 private 或 shared' '--mode must be private or shared') }
if ($Help) {
    Write-Host (T '首次使用：.\scripts\start.ps1 --mode shared [--dir <节点目录>]' 'First run: .\scripts\start.ps1 --mode shared [--dir <node-dir>]')
    Write-Host (T '自动流程：按需构建 -> 仅首次初始化 -> 托盘启动 -> 显示管理台地址。' 'Flow: build if needed -> initialise on first run only -> start in tray -> print console URL.')
    Write-Host (T '--mode 选填，默认 private；shared 使用发布者授权，不生成 swarm.key。已有目录不改模式。' '--mode is optional, default private; shared uses publisher authorisation and creates no swarm.key. Existing directories keep their mode.')
    Write-Host (T '--dir 选填，默认用户目录下 .noobloft；`--` 之后的 init 参数只用于首次初始化。' '--dir is optional, default .noobloft in your home directory; init arguments after `--` apply to the first run only.')
    Write-Host (T '启动后用节点目录 admin.token 登录，进入「资源管理与使用说明」。' 'After start, sign in with admin.token from the node directory and open "Resources and usage".')
    Write-Host (T '输出语言：设置 NOOBLOFT_LANG=en 或 zh-CN。' 'Output language: set NOOBLOFT_LANG=en or zh-CN.')
    return
}
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8

$repoDir = Split-Path -Parent $PSScriptRoot
$exe = Join-Path $repoDir 'bin\noobloftd.exe'
$trayExe = Join-Path $repoDir 'bin\noobloftd-tray.exe'
$Dir = [System.IO.Path]::GetFullPath($Dir)

function Get-TrayProcess {
    Get-CimInstance Win32_Process -Filter "Name='noobloftd-tray.exe' OR Name='noobloftd.exe'" |
        Where-Object { ($_.Name -eq 'noobloftd-tray.exe' -or $_.CommandLine -match '\stray(\s|$)') -and $_.CommandLine -like "*$Dir*" }
}

function Get-PortListenerPid {
    param([int]$Port)
    if (Get-Command Get-NetTCPConnection -ErrorAction SilentlyContinue) {
        $conn = Get-NetTCPConnection -State Listen -LocalPort $Port -ErrorAction SilentlyContinue | Select-Object -First 1
        if ($conn) { return [int]$conn.OwningProcess }
        return $null
    }
    $line = netstat -ano -p tcp | Select-String -Pattern ":$Port\s+\S+\s+LISTENING\s+(\d+)" | Select-Object -First 1
    if ($line) { return [int]$line.Matches[0].Groups[1].Value }
    return $null
}

function Test-BuildStale {
    if (-not (Test-Path $exe) -or -not (Test-Path $trayExe)) { return $true }
    $built = @((Get-Item $exe).LastWriteTimeUtc, (Get-Item $trayExe).LastWriteTimeUtc) | Sort-Object | Select-Object -First 1
    $sources = @('cmd', 'internal', 'locales', 'web\src', 'web\index.html', 'web\package.json', 'web\package-lock.json', 'go.mod', 'go.sum', 'scripts\build.ps1', 'scripts\start.ps1') |
        ForEach-Object { Join-Path $repoDir $_ } | Where-Object { Test-Path $_ }
    $newer = Get-ChildItem -Path $sources -Recurse -File |
        Where-Object { $_.FullName -notlike '*\internal\controlplane\webui\dist\*' -and $_.LastWriteTimeUtc -gt $built } |
        Select-Object -First 1
    return [bool]$newer
}

$running = Get-TrayProcess
if (Test-BuildStale) {
    if ($running) {
        Write-Warning (T '源码比二进制新，但托盘正在运行（exe 被占用），本次不重建。要应用新版本：托盘图标右键「退出」后重新运行本脚本。' 'Sources are newer than the binaries, but the tray is running (exe in use), so no rebuild. To apply the new version, right-click the tray icon, choose Quit, then rerun this script.')
        if (-not (Test-Path $trayExe)) { return }
    } else {
        Write-Host (T '== 构建（二进制缺失或比源码旧）' '== Build (binaries missing or older than sources)')
        & (Join-Path $PSScriptRoot 'build.ps1')
    }
} else {
    Write-Host (T '== 构建：二进制已是最新，跳过' '== Build: binaries up to date, skipped')
}

$configPath = Join-Path $Dir 'config.json'
if (Test-Path $configPath) {
    Write-Host (T "== 初始化：已存在 $configPath，跳过" "== Init: $configPath exists, skipped")
    if ($modeSpecified) { Write-Host (T '已有配置：--mode 不修改保存的网络模式。' 'Existing config: --mode does not change the saved network mode.') }
} else {
    Write-Host (T "== 初始化节点目录 $Dir" "== Initialising node directory $Dir")
    & $exe init -dir $Dir -mode $Mode @InitArgs
    if ($LASTEXITCODE -ne 0) { throw (T "init 失败（退出码 $LASTEXITCODE）。如果目录里残留了半成品文件，确认无用后清理再重试。" "init failed (exit code $LASTEXITCODE). If partial files remain in the directory, remove them once confirmed unused and retry.") }
}

if ($running) {
    Write-Host (T '== 托盘已在运行，唤出已有状态窗口' '== Tray already running, showing the existing status window')
} else {
    Write-Host (T '== 在托盘后台启动节点' '== Starting the node in the tray')
}
Start-Process -FilePath $trayExe -ArgumentList @('-dir', "`"$Dir`"") -WindowStyle Hidden | Out-Null

$config = Get-Content -Raw -Encoding UTF8 $configPath | ConvertFrom-Json
$bind = $config.gateway.bindAddr -replace '^(0\.0\.0\.0|\[::\]):', '127.0.0.1:'
$url = "http://$bind/"
if (-not $running -and $config.gateway.enabled) {
    $port = 0
    if ($bind -match ':(\d+)$') { $port = [int]$Matches[1] }
    $tray = $null
    $deadline = (Get-Date).AddSeconds(30)
    $ok = $false
    while ((Get-Date) -lt $deadline) {
        if ($null -eq $tray) { $tray = Get-TrayProcess | Select-Object -First 1 }
        $owner = if ($port -gt 0) { Get-PortListenerPid -Port $port } else { $null }
        if ($tray -and $owner -and $owner -eq $tray.ProcessId) {
            try { Invoke-WebRequest -UseBasicParsing -TimeoutSec 2 ($url + 'healthz') | Out-Null; $ok = $true; break } catch { }
        }
        Start-Sleep -Milliseconds 500
    }
    if (-not $ok) {
        if ($port -gt 0) {
            $owner = Get-PortListenerPid -Port $port
            if ($owner -and (-not $tray -or $owner -ne $tray.ProcessId)) {
                $other = Get-CimInstance Win32_Process -Filter "ProcessId=$owner" -ErrorAction SilentlyContinue
                $name = if ($other) { $other.Name } else { T '未知进程' 'unknown process' }
                Write-Warning (T "30 秒内网关未就绪：端口 $port 被 PID $owner（$name）占用，不是本次启动的托盘进程。请改 config.json 的 gateway.bindAddr，或先停掉占用者。" "Gateway not ready within 30s: port $port is held by PID $owner ($name), not the tray started here. Change gateway.bindAddr in config.json or stop that process first.")
                exit 1
            }
        }
        Write-Warning (T "30 秒内网关未就绪。点击右下角托盘图标查看状态，日志见 $(Join-Path $Dir 'node.log')" "Gateway not ready within 30s. Click the tray icon for status; log: $(Join-Path $Dir 'node.log')")
        exit 1
    }
}
Write-Host ''
Write-Host ((T '节点目录: ' 'Node dir: ') + $Dir)
if ($config.gateway.enabled) { Write-Host ((T '管理台  : ' 'Console : ') + $url + (T ' （登录凭证: ' ' (sign-in token: ') + (Join-Path $Dir 'admin.token') + (T '）' ')')) }
Write-Host ((T '日志    : ' 'Log     : ') + (Join-Path $Dir 'node.log'))
Write-Host (T '托盘图标: 单击查看运行状态；关闭窗口只隐藏到托盘；右键「退出」或窗口里「停止并退出」才会停止节点。' 'Tray icon: click for status; closing the window only hides it; right-click Quit or "Stop and exit" in the window stops the node.')
