#!/usr/bin/env pwsh
# check-env.ps1 — verify the local development environment.
# Friendly diagnostic tool for Windows PowerShell users. For non-Windows
# or for the canonical entry point, see `make env` in the Makefile.
#
# Run:  pwsh scripts/check-env.ps1
# Or:   .\scripts\check-env.ps1

$ErrorActionPreference = 'Continue'

# Color helpers (with fallback for non-color terminals).
function Write-Ok    { param($msg) Write-Host "  [OK]    " -NoNewline -ForegroundColor Green;  Write-Host $msg }
function Write-Warn  { param($msg) Write-Host "  [WARN]  " -NoNewline -ForegroundColor Yellow; Write-Host $msg }
function Write-Fail  { param($msg) Write-Host "  [FAIL]  " -NoNewline -ForegroundColor Red;    Write-Host $msg }
function Write-Info  { param($msg) Write-Host "  [INFO]  " -NoNewline -ForegroundColor Gray;   Write-Host $msg }

$Script:Pass = 0
$Script:Warn = 0
$Script:Fail = 0

function Test-Ok    { param($msg) $Script:Pass++; Write-Ok   $msg }
function Test-Warn  { param($msg) $Script:Warn++; Write-Warn $msg }
function Test-Fail  { param($msg) $Script:Fail++; Write-Fail $msg }

function Test-Command {
    param([string]$Name, [string]$DisplayName = $Name, [switch]$Required = $false)
    $cmd = Get-Command $Name -ErrorAction SilentlyContinue
    if ($cmd) {
        Test-Ok "$DisplayName found ($($cmd.Source))"
    } elseif ($Required) {
        Test-Fail "$DisplayName not found (required)"
    } else {
        Test-Warn "$DisplayName not found (optional)"
    }
}

function Test-Network {
    param([string]$Name, [string]$Url)
    try {
        $r = Invoke-WebRequest -Uri $Url -UseBasicParsing -TimeoutSec 5 -Method Head
        Test-Ok "$Name reachable (HTTP $($r.StatusCode))"
    } catch {
        Test-Warn "$Name unreachable: $($_.Exception.Message)"
    }
}

# ---------- header ----------
Write-Host ""
Write-Host "=== peekdb development environment check ===" -ForegroundColor Cyan
Write-Host "  (this script is informational; failure does not block 'make build')"
Write-Host ""

# ---------- Go toolchain ----------
Write-Host "Go toolchain:" -ForegroundColor Yellow

$goCmd = Get-Command go -ErrorAction SilentlyContinue
if (-not $goCmd) {
    Test-Fail "go not found in PATH"
} else {
    Test-Ok "go found ($($goCmd.Source))"
    $goVersionOutput = & go version 2>&1
    if ($LASTEXITCODE -eq 0) {
        $versionLine = ($goVersionOutput | Select-Object -First 1) -as [string]
        Test-Ok $versionLine

        # Parse "go version goX.Y.Z ..." and warn if older than 1.22.
        if ($versionLine -match 'go(\d+)\.(\d+)') {
            $major = [int]$Matches[1]
            $minor = [int]$Matches[2]
            if ($major -lt 1 -or ($major -eq 1 -and $minor -lt 22)) {
                Test-Warn "Go $major.$minor is older than 1.22 (peekdb minimum)"
            }
        }
    } else {
        Test-Fail "go version failed"
    }
}

Test-Command "gofmt"          "gofmt (bundled)"
Test-Command "goimports"      "goimports (recommended)"
Test-Command "golangci-lint"  "golangci-lint v2 (recommended)"
Test-Command "dlv"            "delve (optional, debugger)"
Test-Command "goreleaser"     "goreleaser v2 (optional, for snapshot releases)"
Write-Host ""

# ---------- Go environment ----------
Write-Host "Go environment:" -ForegroundColor Yellow

$proxy = go env GOPROXY 2>$null
if ($LASTEXITCODE -eq 0 -and $proxy) {
    if ($proxy -match 'goproxy\.cn|goproxy\.io|aliyun\.com|tencent\.com') {
        Test-Ok "GOPROXY=$proxy"
    } elseif ($proxy -match 'proxy\.golang\.org') {
        Test-Warn "GOPROXY=$proxy (国内访问可能慢，建议改为 goproxy.cn)"
    } else {
        Test-Ok "GOPROXY=$proxy"
    }
} else {
    Test-Fail "GOPROXY not set"
}

$sumdb = go env GOSUMDB 2>$null
Write-Info "GOSUMDB=$sumdb"
$modcache = go env GOMODCACHE 2>$null
Write-Info "GOMODCACHE=$modcache"
$gocache = go env GOCACHE 2>$null
Write-Info "GOCACHE=$gocache"
Write-Host ""

# ---------- Network ----------
Write-Host "Network (proxy reachability):" -ForegroundColor Yellow
Test-Network "goproxy.cn" "https://goproxy.cn"
Test-Network "goproxy.io" "https://goproxy.io"
Test-Network "github.com" "https://github.com"
Write-Host ""

# ---------- Project sanity ----------
Write-Host "Project sanity:" -ForegroundColor Yellow

if (-not (Test-Path "go.mod")) {
    Test-Fail "go.mod not found — run from the project root"
} else {
    Test-Ok "go.mod found"

    $tmpBin = Join-Path $env:TEMP "peekdb-envcheck-$PID.exe"
    try {
        $null = go vet ./... 2>&1
        if ($LASTEXITCODE -eq 0) {
            Test-Ok "go vet ./... clean"
        } else {
            Test-Fail "go vet ./... has issues"
        }

        $null = go build -o $tmpBin ./cmd/peekdb 2>&1
        if ($LASTEXITCODE -eq 0) {
            Test-Ok "go build ./cmd/peekdb OK"
        } else {
            Test-Fail "go build ./cmd/peekdb failed"
        }
    } finally {
        Remove-Item $tmpBin -ErrorAction SilentlyContinue
    }
}
Write-Host ""

# ---------- Summary ----------
Write-Host "=== summary ===" -ForegroundColor Cyan
Write-Host "  pass:  $Script:Pass" -ForegroundColor Green
Write-Host "  warn:  $Script:Warn" -ForegroundColor Yellow
Write-Host "  fail:  $Script:Fail" -ForegroundColor Red
Write-Host ""

# Decision logic: pass with warnings is OK; only hard failures exit non-zero.
# `make env-full` (which calls this script) does NOT block builds.
if ($Script:Fail -gt 0) {
    Write-Host "There are $Script:Fail blocking issues. See DEVELOPING.md for setup." -ForegroundColor Red
    exit 1
} else {
    Write-Host "No blocking issues. Warnings above are recommendations only." -ForegroundColor Green
    exit 0
}
