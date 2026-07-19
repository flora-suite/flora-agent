$ErrorActionPreference = 'Stop'

$Repository = 'flora-suite/flora-agent'
$BinaryName = 'flora-agent'

function Get-Architecture {
    $architecture = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString()
    switch ($architecture) {
        'X64' { return 'amd64' }
        'Arm64' { return 'arm64' }
        default { throw "Unsupported Windows architecture: $architecture" }
    }
}

$release = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repository/releases/latest"
$tag = $release.tag_name
if ([string]::IsNullOrWhiteSpace($tag)) {
    throw 'Could not determine the latest release.'
}

$version = $tag.TrimStart('v')
$architecture = Get-Architecture
$archive = "${BinaryName}_${version}_windows_${architecture}.zip"
$checksumAsset = $release.assets | Where-Object { $_.name -eq 'checksums.txt' } | Select-Object -First 1
$archiveAsset = $release.assets | Where-Object { $_.name -eq $archive } | Select-Object -First 1
if ($null -eq $archiveAsset -or $null -eq $checksumAsset) {
    throw "Release $tag does not contain the required Windows $architecture assets."
}

$temporaryDirectory = Join-Path ([System.IO.Path]::GetTempPath()) ("flora-agent-" + [Guid]::NewGuid())
$installDirectory = Join-Path $env:LOCALAPPDATA 'Programs\flora-agent'
New-Item -ItemType Directory -Force -Path $temporaryDirectory | Out-Null

try {
    $archivePath = Join-Path $temporaryDirectory $archive
    $checksumsPath = Join-Path $temporaryDirectory 'checksums.txt'
    Invoke-WebRequest -Uri $archiveAsset.browser_download_url -OutFile $archivePath
    Invoke-WebRequest -Uri $checksumAsset.browser_download_url -OutFile $checksumsPath

    $checksumLine = Get-Content $checksumsPath | Where-Object { $_ -match ("\\s" + [regex]::Escape($archive) + '$') } | Select-Object -First 1
    $expected = $checksumLine.Split()[0]
    if ([string]::IsNullOrWhiteSpace($expected)) {
        throw "Checksum for $archive is missing."
    }
    $actual = (Get-FileHash -Algorithm SHA256 $archivePath).Hash.ToLowerInvariant()
    if ($expected.ToLowerInvariant() -ne $actual) {
        throw 'Checksum verification failed.'
    }

    New-Item -ItemType Directory -Force -Path $installDirectory | Out-Null
    Expand-Archive -Path $archivePath -DestinationPath $installDirectory -Force
}
finally {
    Remove-Item -Recurse -Force -ErrorAction SilentlyContinue $temporaryDirectory
}

$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if (($userPath -split ';') -notcontains $installDirectory) {
    [Environment]::SetEnvironmentVariable('Path', (($userPath.TrimEnd(';') + ';' + $installDirectory).TrimStart(';')), 'User')
}

Write-Host "Installed $BinaryName $tag to $installDirectory"
Write-Host 'Open a new PowerShell window before running flora-agent.'
