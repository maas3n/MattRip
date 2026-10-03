$ErrorActionPreference = 'Stop'

$required = [Version]'1.27.1'
$actualText = (& go version)
if ($actualText -notmatch 'go([0-9]+\.[0-9]+(?:\.[0-9]+)?)') {
    throw "Could not determine Go version. Install Go 1.27.1 or newer."
}
$actual = [Version]$Matches[1]
if ($actual -lt $required) {
    throw "MattRip release builds require Go 1.27.1 or newer. Found $actual."
}

$buildVersion = if ([string]::IsNullOrWhiteSpace($env:MATTRIP_VERSION)) { 'dev' } else { $env:MATTRIP_VERSION }

$env:CGO_ENABLED = '0'
$env:GOOS = 'windows'
$env:GOARCH = 'amd64'

go test ./...
go build -trimpath -buildvcs=false -ldflags "-s -w -H=windowsgui -X main.appVersion=$buildVersion" -o ..\MattRip.exe .
go build -trimpath -buildvcs=false -tags cli -ldflags "-s -w -X main.appVersion=$buildVersion" -o ..\mattrip-cli.exe .
Copy-Item .\MattRip.exe.manifest ..\MattRip.exe.manifest -Force
Get-FileHash ..\MattRip.exe -Algorithm SHA256
Get-FileHash ..\mattrip-cli.exe -Algorithm SHA256
