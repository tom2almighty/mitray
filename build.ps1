param([string]$Version = "dev")
$ErrorActionPreference = "Stop"
$oldGOOS = $env:GOOS
$oldGOARCH = $env:GOARCH
$oldCGO = $env:CGO_ENABLED
Push-Location $PSScriptRoot
try {
    Remove-Item Env:GOOS -ErrorAction SilentlyContinue
    Remove-Item Env:GOARCH -ErrorAction SilentlyContinue
    go run ./tools/resources -arch amd64
    if ($LASTEXITCODE -ne 0) { throw "Resource generation failed" }
    $env:GOOS = "windows"
    $env:GOARCH = "amd64"
    $env:CGO_ENABLED = "0"
    go build -trimpath -ldflags "-H windowsgui -s -w -X main.version=$Version" -o dist/mitray.exe ./cmd/mitray
    if ($LASTEXITCODE -ne 0) { throw "Build failed" }
} finally {
    $env:GOOS = $oldGOOS
    $env:GOARCH = $oldGOARCH
    $env:CGO_ENABLED = $oldCGO
    Pop-Location
}
