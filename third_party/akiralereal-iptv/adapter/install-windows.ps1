# SPDX-License-Identifier: GPL-3.0-only
param([string]$RuntimeToolsDir = '', [switch]$SkipBrowser, [switch]$SkipFFmpeg)
$ErrorActionPreference = 'Stop'
$taskSourceRoot = Split-Path -Parent $PSScriptRoot
$taskProjectRoot = Split-Path -Parent (Split-Path -Parent $taskSourceRoot)
if (-not $RuntimeToolsDir) { $RuntimeToolsDir = Join-Path (Split-Path -Parent (Split-Path -Parent $taskSourceRoot)) 'data\iptv\tools' }
$taskToolsRoot = [IO.Path]::GetFullPath($RuntimeToolsDir)
New-Item -ItemType Directory -Path $taskToolsRoot -Force | Out-Null
$taskNode = Get-Command node -ErrorAction SilentlyContinue
$taskNodeMajor = if ($taskNode) { [int]((& $taskNode.Source -p 'process.versions.node.split(String.fromCharCode(46))[0]') | Select-Object -First 1) } else { 0 }
if ($taskNodeMajor -lt 24) {
    $taskChecksums = (Invoke-WebRequest -UseBasicParsing 'https://nodejs.org/dist/latest-v24.x/SHASUMS256.txt').Content
    $taskNodeArch = if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64') { 'arm64' } else { 'x64' }
    $taskMatch = [regex]::Match($taskChecksums, '(?m)^([0-9a-f]{64})\s+(node-v24\.\d+\.\d+-win-' + $taskNodeArch + '\.zip)\s*$')
    if (-not $taskMatch.Success) { throw 'Official Node 24 archive checksum not found' }
    $taskArchiveName = $taskMatch.Groups[2].Value
    $taskArchive = Join-Path $taskToolsRoot $taskArchiveName
    Invoke-WebRequest -UseBasicParsing ('https://nodejs.org/dist/latest-v24.x/' + $taskArchiveName) -OutFile $taskArchive
    if ((Get-FileHash -LiteralPath $taskArchive -Algorithm SHA256).Hash.ToLowerInvariant() -ne $taskMatch.Groups[1].Value) { throw 'Node archive checksum mismatch' }
    Expand-Archive -LiteralPath $taskArchive -DestinationPath $taskToolsRoot -Force
    $taskNodeDir = Join-Path $taskToolsRoot ($taskArchiveName -replace '\.zip$', '')
    $env:PATH = $taskNodeDir + ';' + $env:PATH
}
if (-not $SkipBrowser) {
    $taskChrome = @('C:\Program Files\Google\Chrome\Application\chrome.exe', 'C:\Program Files (x86)\Google\Chrome\Application\chrome.exe', 'C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe') | Where-Object { Test-Path -LiteralPath $_ } | Select-Object -First 1
    if (-not $taskChrome) {
        if (-not (Get-Command winget -ErrorAction SilentlyContinue)) { throw 'Install Chrome or provide PUPPETEER_EXECUTABLE_PATH; winget is unavailable' }
        & winget install --id Google.Chrome --exact --silent --accept-package-agreements --accept-source-agreements
        if ($LASTEXITCODE -ne 0) { throw 'Chrome dependency installation failed' }
    } else { $env:PUPPETEER_EXECUTABLE_PATH = $taskChrome }
}
$taskFFmpegCommand = Get-Command ffmpeg -ErrorAction SilentlyContinue
$taskBundledFFmpeg = Join-Path $taskProjectRoot 'resource\static\suxinvideo\bin\windows-amd64\ffmpeg.exe'
$taskFFmpegBinary = if ($env:SUXIN_FFMPEG -and (Test-Path -LiteralPath $env:SUXIN_FFMPEG)) { $env:SUXIN_FFMPEG } elseif ($taskFFmpegCommand) { $taskFFmpegCommand.Source } elseif (Test-Path -LiteralPath $taskBundledFFmpeg) { $taskBundledFFmpeg } else { '' }
if (-not $SkipFFmpeg -and -not $taskFFmpegBinary) {
    if (-not (Get-Command winget -ErrorAction SilentlyContinue)) { throw 'Install FFmpeg or provide its binary to the Go runtime; winget is unavailable' }
    & winget install --id Gyan.FFmpeg --exact --silent --accept-package-agreements --accept-source-agreements
    if ($LASTEXITCODE -ne 0) { throw 'FFmpeg dependency installation failed' }
    $env:PATH = (Join-Path $env:LOCALAPPDATA 'Microsoft\WinGet\Links') + ';' + $env:PATH
    $taskFFmpegCommand = Get-Command ffmpeg -ErrorAction SilentlyContinue
    if ($taskFFmpegCommand) { $taskFFmpegBinary = $taskFFmpegCommand.Source }
}
$env:PUPPETEER_SKIP_DOWNLOAD = 'true'
Push-Location $taskSourceRoot
try {
    & npm.cmd ci --omit=dev --no-audit --no-fund
    if ($LASTEXITCODE -ne 0) { throw 'Node dependency installation failed' }
    & node --test adapter/adapter.test.mjs
    if ($LASTEXITCODE -ne 0) { throw 'Adapter verification failed' }
} finally { Pop-Location }
Write-Output 'Dependencies ready. Node processes are started by the Go supervisor.'
$taskNodeBinary = (Get-Command node).Source
Write-Output ('SUXIN_NODE=' + $taskNodeBinary)
if ($taskFFmpegBinary) { Write-Output ('SUXIN_FFMPEG=' + $taskFFmpegBinary) }
Write-Output 'Runtime tools and node_modules must stay outside every release/development archive.'
