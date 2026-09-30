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
$TimeoutSeconds = 30
$i = 0
while ($i -lt $CliArgs.Count) {
    $arg = $CliArgs[$i]
    $needValue = { param($name) if ($i + 1 -ge $CliArgs.Count) { throw (T "$name 需要一个值" "$name requires a value") } ; $CliArgs[$i + 1] }
    if ($arg -match '^--?dir$') { $Dir = & $needValue $arg; $i += 2 }
    elseif ($arg -match '^--?timeout(-?seconds)?$') { $TimeoutSeconds = [int](& $needValue $arg); $i += 2 }
    elseif ($arg -match '^--?(h|help|\?)$') {
        Write-Host (T '用法: .\scripts\stop.ps1 [--dir <节点目录>] [--timeout <秒>]' 'Usage: .\scripts\stop.ps1 [--dir <node-dir>] [--timeout <seconds>]')
        Write-Host (T '--dir 选填，默认用户目录下 .noobloft；--timeout 选填，默认 30 秒，范围 1-300。' '--dir is optional, default .noobloft in your home directory; --timeout is optional, default 30, range 1-300 seconds.')
        return
    }
    else { throw (T "未知参数: $arg" "Unknown argument: $arg") }
}
if ($TimeoutSeconds -lt 1 -or $TimeoutSeconds -gt 300) { throw (T '--timeout 必须在 1-300 秒之间' '--timeout must be between 1 and 300 seconds') }
$Dir = [System.IO.Path]::GetFullPath($Dir)
if (-not ('SwarmStopWindowV2' -as [type])) {
Add-Type @'
using System;
using System.Runtime.InteropServices;
using System.Text;
public static class SwarmStopWindowV2 {
    [DllImport("shell32.dll", CharSet=CharSet.Unicode, SetLastError=true)] static extern IntPtr CommandLineToArgvW(string command, out int count);
    [DllImport("kernel32.dll")] static extern IntPtr LocalFree(IntPtr pointer);
    public static string[] Arguments(string command) {
        int count;
        IntPtr pointer = CommandLineToArgvW(command, out count);
        if (pointer == IntPtr.Zero) throw new System.ComponentModel.Win32Exception();
        try {
            string[] result = new string[count];
            for (int i = 0; i < count; i++) result[i] = Marshal.PtrToStringUni(Marshal.ReadIntPtr(pointer, i * IntPtr.Size));
            return result;
        } finally { LocalFree(pointer); }
    }
    public delegate bool EnumProc(IntPtr hwnd, IntPtr param);
    [DllImport("user32.dll")] public static extern bool EnumWindows(EnumProc callback, IntPtr param);
    [DllImport("user32.dll", CharSet=CharSet.Unicode)] public static extern int GetClassName(IntPtr hwnd, StringBuilder text, int count);
    [DllImport("user32.dll", CharSet=CharSet.Unicode)] public static extern int GetWindowText(IntPtr hwnd, StringBuilder text, int count);
    [DllImport("user32.dll")] public static extern uint GetWindowThreadProcessId(IntPtr hwnd, out uint pid);
    [DllImport("user32.dll", SetLastError=true)] public static extern bool PostMessage(IntPtr hwnd, uint msg, IntPtr wp, IntPtr lp);
}
'@
}
$targets = [System.Collections.Generic.List[object]]::new()
$callback = [SwarmStopWindowV2+EnumProc]{
    param($hwnd, $unused)
    $class = [System.Text.StringBuilder]::new(256)
    [void][SwarmStopWindowV2]::GetClassName($hwnd, $class, $class.Capacity)
    if ($class.ToString() -eq 'NoobloftTrayWindow') {
        $title = [System.Text.StringBuilder]::new(32768)
        [void][SwarmStopWindowV2]::GetWindowText($hwnd, $title, $title.Capacity)
        $prefix = 'Noobloft Node - '
        if ($title.ToString().StartsWith($prefix)) {
            $path = [System.IO.Path]::GetFullPath($title.ToString().Substring($prefix.Length))
            if ($path.TrimEnd('\') -ieq $Dir.TrimEnd('\')) {
                [uint32]$ownerId = 0
                [void][SwarmStopWindowV2]::GetWindowThreadProcessId($hwnd, [ref]$ownerId)
                $process = Get-Process -Id $ownerId -ErrorAction SilentlyContinue
                if ($process) { $targets.Add(@{ Window = $hwnd; Process = $process }) }
            }
        }
    }
    return $true
}
[void][SwarmStopWindowV2]::EnumWindows($callback, [IntPtr]::Zero)
function Stop-CommandLineNodes {
    $expectedExe = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..\bin\noobloftd.exe'))
    foreach ($candidate in @(Get-CimInstance Win32_Process -Filter "Name='noobloftd.exe'")) {
        if ($candidate.ExecutablePath -ine $expectedExe -or -not $candidate.CommandLine) { continue }
        $argv = [SwarmStopWindowV2]::Arguments($candidate.CommandLine)
        if ($argv.Length -lt 2 -or $argv[1] -cne 'run') { continue }
        $nodeDir = $null
        $valid = $true
        for ($i = 2; $i -lt $argv.Length; $i++) {
            if ($argv[$i] -cin @('-dir', '--dir') -and $i + 1 -lt $argv.Length) {
                $nodeDir = $argv[++$i]
            } elseif ($argv[$i] -cmatch '^--?dir=(.*)$') {
                $nodeDir = $Matches[1]
            } else { $valid = $false; break }
        }
        if (-not $valid -or -not $nodeDir -or $nodeDir -notmatch '^(?:[a-zA-Z]:[\\/]|\\\\)') { continue }
        if ([IO.Path]::GetFullPath($nodeDir).TrimEnd('\') -ine $Dir.TrimEnd('\')) { continue }
        $process = Get-Process -Id $candidate.ProcessId -ErrorAction SilentlyContinue
        if (-not $process) { continue }
        $null = $process.Handle
        if ($process.HasExited) { continue }
        if ($process.Path -ine $expectedExe -or [Math]::Abs($process.StartTime.ToUniversalTime().Ticks - $candidate.CreationDate.ToUniversalTime().Ticks) -ge 10) {
            throw (T "PID $($candidate.ProcessId) 的进程身份已变化，未尝试终止。" "Process identity changed for PID $($candidate.ProcessId); no termination attempted.")
        }
        Write-Warning (T "正在停止命令行节点 PID $($process.Id): $Dir。进行中的请求会被中断，未落盘的数据可能丢失。" "Stopping command-line node PID $($process.Id): $Dir. Active requests will be interrupted; buffered data may be lost.")
        $process.Kill()
        if (-not $process.WaitForExit($TimeoutSeconds * 1000)) { throw (T "命令行节点 PID $($process.Id) 未在超时内退出。" "Command-line node PID $($process.Id) did not exit in time.") }
    }
}
function Assert-GatewayStopped {
    $configPath = Join-Path $Dir 'config.json'
    if (-not (Test-Path $configPath)) { return }
    $config = Get-Content -Raw -Encoding UTF8 $configPath | ConvertFrom-Json
    if (-not $config.gateway.enabled) { return }
    $endpoint = [Uri]("http://" + $config.gateway.bindAddr)
    $listeners = @(Get-NetTCPConnection -State Listen -ErrorAction Stop | Where-Object {
        $_.LocalPort -eq $endpoint.Port -and
        ($_.LocalAddress -eq $endpoint.Host.Trim('[', ']') -or
         $_.LocalAddress -in @('0.0.0.0', '::') -or
         $endpoint.Host.Trim('[', ']') -in @('0.0.0.0', '::'))
    })
    foreach ($listener in $listeners) {
        $owner = Get-CimInstance Win32_Process -Filter "ProcessId=$($listener.OwningProcess)"
        Write-Warning (T "网关 $($config.gateway.bindAddr) 仍被 PID $($listener.OwningProcess) ($($owner.Name)) 占用。" "Gateway $($config.gateway.bindAddr) is still occupied by PID $($listener.OwningProcess) ($($owner.Name)).")
    }
    if ($listeners.Count -gt 0) {
        throw (T '网关端口仍被占用，且无法安全停止占用者。请检查其可执行文件和节点目录；无关进程不会被终止。' 'Gateway is still occupied. The owner could not be safely stopped. Check its executable and node directory; unrelated processes are never terminated.')
    }
}
if ($targets.Count -eq 0) {
    Stop-CommandLineNodes
    Assert-GatewayStopped
    Write-Host (T "已停止或原本未运行: $Dir" "Stopped or already stopped: $Dir")
    exit 0
}
foreach ($target in $targets) {
    if (-not [SwarmStopWindowV2]::PostMessage($target.Window, 0x8005, [IntPtr]::Zero, [IntPtr]::Zero)) {
        if ($target.Process.HasExited) { continue }
        throw (T '无法发送停止请求，请以与托盘相同的权限运行。' 'Cannot send stop request. Run with the same privileges as the tray.')
    }
    if (-not $target.Process.WaitForExit($TimeoutSeconds * 1000)) {
        Write-Warning (T "托盘 PID $($target.Process.Id) 未响应脚本停止，正在请求其退出操作；如弹出确认框请确认。" "Tray PID $($target.Process.Id) did not respond to scripted stop. Requesting its Exit action; confirm the tray dialog if shown.")
        [void][SwarmStopWindowV2]::PostMessage($target.Window, 0x8003, [IntPtr]::Zero, [IntPtr]::Zero)
        if (-not [SwarmStopWindowV2]::PostMessage($target.Window, 0x0111, [IntPtr]204, [IntPtr]::Zero)) {
            if ($target.Process.HasExited) { continue }
            throw (T '无法请求托盘退出，请以与托盘相同的权限运行。' 'Cannot request tray Exit. Run with the same privileges as the tray.')
        }
        if (-not $target.Process.WaitForExit($TimeoutSeconds * 1000)) {
            throw (T "托盘 PID $($target.Process.Id) 仍未退出。请确认其退出对话框，若停机卡住请查看 node.log。未强制终止任何进程。" "Tray PID $($target.Process.Id) has not exited. Confirm its Exit dialog, or inspect node.log if shutdown is stalled. No process was forcibly terminated.")
        }
    }
}
Stop-CommandLineNodes
Assert-GatewayStopped
Write-Host (T "已停止: $Dir" "Stopped: $Dir")
