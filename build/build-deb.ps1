$env:GOROOT = ""
Set-Location "D:\easytierpro\easytier-pro-gui"

# Package version = bundled easytier-core version (e.g. "2.6.4" from
# "easytier-core 2.6.4-8428a89d"), override with $env:DEB_VERSION.
$ver = $env:DEB_VERSION
if (-not $ver) {
    $banner = (& "resources\bin\easytier-core.exe" --version) 2>$null
    if ($banner -match '(\d+\.\d+\.\d+)') { $ver = $Matches[1] }
}
if (-not $ver) { $ver = "0.0" }
Write-Output "deb version: $ver"

Write-Output "=== linux amd64 server ==="
$env:GOOS = "linux"; $env:GOARCH = "amd64"; $env:CGO_ENABLED = "0"
go build -trimpath -tags headless -ldflags "-s -w" -o "build\bin\easytier-pro-server-linux-amd64" .
if ($LASTEXITCODE -ne 0) { Write-Output "AMD64 BUILD FAILED"; exit 1 }

Write-Output "=== linux arm64 server ==="
$env:GOARCH = "arm64"
go build -trimpath -tags headless -ldflags "-s -w" -o "build\bin\easytier-pro-server-linux-arm64" .
if ($LASTEXITCODE -ne 0) { Write-Output "ARM64 BUILD FAILED"; exit 1 }

Write-Output "=== mkdeb tool ==="
$env:GOOS = "windows"; $env:GOARCH = "amd64"
go build -o "build\mkdeb.exe" ./build/mkdeb
if ($LASTEXITCODE -ne 0) { Write-Output "MKDEB BUILD FAILED"; exit 1 }

Write-Output "=== deb amd64 ==="
& "build\mkdeb.exe" -out "build\bin\easytier-pro_$ver`_amd64.deb" -bin "build\bin\easytier-pro-server-linux-amd64" -arch amd64 -version $ver
if ($LASTEXITCODE -ne 0) { Write-Output "AMD64 DEB FAILED"; exit 1 }

Write-Output "=== deb arm64 ==="
& "build\mkdeb.exe" -out "build\bin\easytier-pro_$ver`_arm64.deb" -bin "build\bin\easytier-pro-server-linux-arm64" -arch arm64 -version $ver
if ($LASTEXITCODE -ne 0) { Write-Output "ARM64 DEB FAILED"; exit 1 }

# GUI deb (built via build/wsl-build-gui.sh in WSL Ubuntu-22.04; needs GTK
# runtime deps, so it is only packaged when the binary is present).
if (Test-Path "build\bin\easytier-pro-gui-linux-amd64") {
    Write-Output "=== deb gui amd64 ==="
    & "build\mkdeb.exe" -out "build\bin\easytier-pro-gui_$ver`_amd64.deb" -bin "build\bin\easytier-pro-gui-linux-amd64" -arch amd64 -version $ver -mode gui -ico "build\appicon.png"
    if ($LASTEXITCODE -ne 0) { Write-Output "GUI DEB FAILED"; exit 1 }
}

Write-Output "=== verify ==="
& "build\mkdeb.exe" -list "build\bin\easytier-pro_$ver`_amd64.deb"
& "build\mkdeb.exe" -list "build\bin\easytier-pro_$ver`_arm64.deb"
if (Test-Path "build\bin\easytier-pro-gui_$ver`_amd64.deb") {
    & "build\mkdeb.exe" -list "build\bin\easytier-pro-gui_$ver`_amd64.deb"
}
Write-Output "=== done ==="
