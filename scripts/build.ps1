<#
.SYNOPSIS
  Windows equivalent of the Makefile (run it yourself in PowerShell).

.EXAMPLE
  .\scripts\build.ps1 deps              # go mod tidy + npm install
  .\scripts\build.ps1 dev-api           # Go API on :8080 (config.dev.yaml)
  .\scripts\build.ps1 dev-web           # Vite dev server on :5173
  .\scripts\build.ps1 windows           # dist\bailing-windows-amd64.exe
  .\scripts\build.ps1 linux             # dist\bailing-linux-amd64 (Ubuntu ECS)
  .\scripts\build.ps1 linux -BasePath /bailing
  .\scripts\build.ps1 linux -App music  # music-only edition (all | photos | music)
  .\scripts\build.ps1 desktop-deps      # once: add Wails to desktop/go.mod
  .\scripts\build.ps1 desktop -App music  # desktop\build\bin\bailing-music.exe
  .\scripts\build.ps1 test

  If scripts are blocked:  Set-ExecutionPolicy -Scope CurrentUser RemoteSigned
#>
param(
    [Parameter(Position = 0)]
    [ValidateSet('help', 'deps', 'dev-api', 'dev-web', 'web', 'windows', 'linux', 'linux-arm64', 'mac',
        'desktop-deps', 'desktop', 'desktop-dev', 'test', 'lint', 'fmt', 'clean')]
    [string]$Target = 'help',
    [string]$Config = 'config.dev.yaml',
    [string]$BasePath = '',
    [ValidateSet('all', 'photos', 'music')]
    [string]$App = 'all'
)

$ErrorActionPreference = 'Stop'
$Root = Split-Path -Parent $PSScriptRoot
Set-Location $Root

$Version = (git describe --tags --always --dirty 2>$null)
if (-not $Version) { $Version = 'dev' }
$LdFlags = "-s -w -X main.version=$Version"

function Invoke-Checked([scriptblock]$Block) {
    & $Block
    if ($LASTEXITCODE -ne 0) { throw "command failed with exit code $LASTEXITCODE" }
}

function Build-Web {
    $env:BASE_PATH = $BasePath
    $env:VITE_BAILING_APP = $App
    Invoke-Checked { npm --prefix web run build }
    New-Item -ItemType File -Force -Path 'internal/webui/dist/.gitkeep' | Out-Null
}

function Build-Go([string]$Os, [string]$Arch, [string]$Out) {
    Build-Web
    $env:CGO_ENABLED = '0'
    $env:GOOS = $Os
    $env:GOARCH = $Arch
    try {
        Invoke-Checked { go build -trimpath -ldflags $LdFlags -o "dist/$Out" ./cmd/bailing }
    } finally {
        Remove-Item Env:GOOS, Env:GOARCH -ErrorAction SilentlyContinue
    }
    Write-Host "built dist/$Out" -ForegroundColor Green
}

switch ($Target) {
    'help' { Get-Help $PSCommandPath -Examples }
    'deps' {
        Invoke-Checked { go mod tidy }
        Invoke-Checked { npm --prefix web install }
    }
    'dev-api' { Invoke-Checked { go run ./cmd/bailing serve -config $Config } }
    'dev-web' {
        $env:VITE_BAILING_APP = $App
        Invoke-Checked { npm --prefix web run dev }
    }
    'web' { Build-Web }
    'desktop-deps' {
        Push-Location desktop
        try {
            Invoke-Checked { go get github.com/wailsapp/wails/v2@latest }
            Invoke-Checked { go mod tidy }
        } finally { Pop-Location }
    }
    'desktop' {
        Build-Web
        Copy-Item "desktop/build/appicon-$App.png" 'desktop/build/appicon.png' -Force
        # regenerate icon.ico from this edition's appicon.png
        Remove-Item -Recurse -Force 'desktop/build/windows' -ErrorAction SilentlyContinue
        Push-Location desktop
        try {
            Invoke-Checked { wails build -clean -ldflags "-X main.edition=$App -X main.version=$Version" -o "bailing-$App.exe" }
        } finally { Pop-Location }
    }
    'desktop-dev' {
        Build-Web
        Push-Location desktop
        try {
            Invoke-Checked { wails dev -ldflags "-X main.edition=$App" }
        } finally { Pop-Location }
    }
    'windows' { Build-Go 'windows' 'amd64' 'bailing-windows-amd64.exe' }
    'linux' { Build-Go 'linux' 'amd64' 'bailing-linux-amd64' }
    'linux-arm64' { Build-Go 'linux' 'arm64' 'bailing-linux-arm64' }
    'mac' {
        Build-Go 'darwin' 'arm64' 'bailing-darwin-arm64'
        Build-Go 'darwin' 'amd64' 'bailing-darwin-amd64'
    }
    'test' {
        Invoke-Checked { go test ./... }
        Invoke-Checked { npm --prefix web run check }
    }
    'lint' {
        Invoke-Checked { go vet ./... }
        Invoke-Checked { npm --prefix web run check }
    }
    'fmt' { Invoke-Checked { gofmt -w cmd internal } }
    'clean' {
        Remove-Item -Recurse -Force dist -ErrorAction SilentlyContinue
        Get-ChildItem internal/webui/dist -Exclude .gitkeep | Remove-Item -Recurse -Force
    }
}

