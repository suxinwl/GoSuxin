param([switch]$Apply)
$ErrorActionPreference = 'Stop'
$workspaceRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..')).TrimEnd('\')
# Explicit inventory only: new runtime directories are never selected automatically.
$obsoleteDirectories = @(
    'browser', 'doc-source', 'final-proxy-cache', 'frontend-baseline', 'isolated-config',
    'module-proxy', 'pre-home-cleanup', 'pre-pan123', 'pre-suxin', 'pre-suxinweb', 'pre-v1',
    'proxy-bin', 'proxy-cache', 'proxy-install-final-bin', 'proxy-install-final-cache',
    'public-webdist', 'public-webdist-final', 'release-assets', 'release-cli-smoke',
    'release-source', 'release-source-final', 'remote-bin', 'remote-gopath',
    'remote-module-cache', 'remote-smoke', 'suxin-cli-check', 'suxin-final-smoke',
    'suxin-smoke', 'suxin-v1-smoke', 'suxin-verified'
)
$keepFiles = @('goframepro.exe', 'suxin.exe', 'local-admin.txt', 'go.mod',
    'suxinweb-server.out.log', 'suxinweb-server.err.log')
$runtimeRoot = Join-Path $workspaceRoot 'runtime'
$relativeTargets = @('resource/webadmin')
$relativeTargets += @($obsoleteDirectories | ForEach-Object { 'runtime/' + $_ })
# Old one-off verification files and binary copies; retain current executables and logs.
$relativeTargets += @(Get-ChildItem -LiteralPath $runtimeRoot -File -Force |
    Where-Object { $_.Name -notin $keepFiles -and
        ($_.Extension -in @('.exe','.log','.png','.cjs','.py','.json','.txt','.sql','.conf') -or
         $_.Name -eq 'webcode-updated.zip') } |
    ForEach-Object { 'runtime/' + $_.Name })
$processPaths = @(Get-CimInstance Win32_Process | ForEach-Object {
    if ($_.ExecutablePath) { $_.ExecutablePath }
})
$targets = @()
$skipped = @()
$bytes = [long]0
foreach ($relative in $relativeTargets) {
    $path = [IO.Path]::GetFullPath((Join-Path $workspaceRoot $relative))
    if (-not $path.StartsWith($workspaceRoot + '\', [StringComparison]::OrdinalIgnoreCase)) {
        throw "Target escapes workspace: $relative"
    }
    if (-not (Test-Path -LiteralPath $path)) { continue }
    foreach ($runningPath in $processPaths) {
        if ($runningPath -eq $path -or $runningPath.StartsWith($path + '\', [StringComparison]::OrdinalIgnoreCase)) {
            throw "Target contains a running executable: $relative"
        }
    }
    $item = Get-Item -LiteralPath $path -Force
    $entries = @($item)
    if ($item.PSIsContainer) { $entries += @(Get-ChildItem -LiteralPath $path -Recurse -Force) }
    $links = @($entries | Where-Object { $_.Attributes -band [IO.FileAttributes]::ReparsePoint })
    if ($links.Count) {
        $skipped += $relative
        Write-Warning "Skipped directory containing links; review manually: $relative"
        continue
    }
    foreach ($entry in $entries) {
        if (-not $entry.PSIsContainer) { $bytes += $entry.Length }
    }
    $targets += $path
    Write-Output $relative
}
Write-Output ("Targets: {0}; size: {1:N2} GiB" -f $targets.Count, ($bytes / 1GB))
if ($skipped.Count) { Write-Output ('Skipped: ' + ($skipped -join ', ')) }
if (-not $Apply) {
    Write-Output 'Preview only. Review the list, then run this script with -Apply to delete.'
    exit
}
foreach ($path in $targets) {
    Remove-Item -LiteralPath $path -Recurse -Force
}
Write-Output 'Cleanup completed. Current service, configuration, uploads, and runtime/pan123 are retained.'
