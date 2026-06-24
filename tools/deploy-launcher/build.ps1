$ErrorActionPreference = "Stop"

$oldPath = $env:Path
$env:Path = "C:\Users\Administrator\Downloads\w64devkit\bin;$env:Path"
$env:CGO_ENABLED = "1"

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $scriptDir

Write-Host "Installing rsrc..." -ForegroundColor Cyan
go install github.com/akavel/rsrc@latest -trimpath 2>$null

Write-Host "Generating manifest resource..." -ForegroundColor Cyan
rsrc -manifest app.manifest -o rsrc.syso

$binDir = Join-Path $scriptDir "..\..\bin"
$binPath = Join-Path $binDir "部署.exe"
Write-Host "Building $binPath ..." -ForegroundColor Cyan
go build -trimpath -ldflags="-s -w -H windowsgui" -o $binPath

$upx = Join-Path $scriptDir "upx.exe"
if (Test-Path $upx) {
    Write-Host "Compressing with UPX..." -ForegroundColor Cyan
    & $upx --best --lzma $binPath
}

Write-Host "Done! Output: $binPath" -ForegroundColor Green

$env:Path = $oldPath
