j[CmdletBinding()]
param(
    [string]$Version = "latest",
    [string]$InstallDir = "$env:LOCALAPPDATA\Programs\Husky",
    [bool]$AddToPath = $true
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

$Repo = "husky-scheduler/husky"
$UserAgent = "husky-install-script"
$Headers = @{ "User-Agent" = $UserAgent }
$TempDir = Join-Path ([System.IO.Path]::GetTempPath()) ("husky-install-" + [System.Guid]::NewGuid().ToString())

function Resolve-LatestTag {
    $latestUri = "https://api.github.com/repos/$Repo/releases/latest"
    $releasesUri = "https://api.github.com/repos/$Repo/releases?per_page=1"

    try {
        $latest = Invoke-RestMethod -Uri $latestUri -Headers $Headers
        if ($null -ne $latest -and $latest.tag_name) {
            return $latest.tag_name
        }
    }
    catch {
    }

    $releases = Invoke-RestMethod -Uri $releasesUri -Headers $Headers
    if ($releases -is [System.Array]) {
        if ($releases.Count -gt 0 -and $releases[0].tag_name) {
            return $releases[0].tag_name
        }
    }
    elseif ($null -ne $releases -and $releases.tag_name) {
        return $releases.tag_name
    }

    throw "failed to resolve release version; no published release tag was found"
}

function Add-InstallDirToPath {
    param(
        [Parameter(Mandatory = $true)]
        [string]$PathToAdd
    )

    $currentUserPath = [Environment]::GetEnvironmentVariable("Path", "User")
    $pathEntries = @()
    if (-not [string]::IsNullOrWhiteSpace($currentUserPath)) {
        $pathEntries = $currentUserPath.Split(";", [System.StringSplitOptions]::RemoveEmptyEntries)
    }

    if ($pathEntries -contains $PathToAdd) {
        if (-not (($env:Path -split ";") -contains $PathToAdd)) {
            $env:Path = "$PathToAdd;$env:Path"
        }
        return $false
    }

    $newUserPath = if ([string]::IsNullOrWhiteSpace($currentUserPath)) {
        $PathToAdd
    }
    else {
        "$currentUserPath;$PathToAdd"
    }

    [Environment]::SetEnvironmentVariable("Path", $newUserPath, "User")
    $env:Path = "$PathToAdd;$env:Path"
    return $true
}

try {
    New-Item -ItemType Directory -Path $TempDir -Force | Out-Null

    $tag = if ($Version -eq "latest") {
        Resolve-LatestTag
    }
    elseif ($Version.StartsWith("v")) {
        $Version
    }
    else {
        "v$Version"
    }

    $osArch = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture
    $archiveArch = switch ($osArch) {
        "X64" { "amd64" }
        "Arm64" {
            Write-Warning "Windows ARM64 will use the amd64 release artifact."
            "amd64"
        }
        default { throw "unsupported architecture: $osArch" }
    }

    $archive = "husky_windows_${archiveArch}.tar.gz"
    $baseUrl = "https://github.com/$Repo/releases/download/$tag"
    $archiveUrl = "$baseUrl/$archive"
    $checksumsUrl = "$baseUrl/checksums.txt"
    $archivePath = Join-Path $TempDir $archive
    $checksumsPath = Join-Path $TempDir "checksums.txt"
    $extractDir = Join-Path $TempDir "extract"

    if (-not (Get-Command tar.exe -ErrorAction SilentlyContinue)) {
        throw "tar.exe is required to extract release archives on Windows"
    }

    Write-Host "Downloading $archiveUrl"
    Invoke-WebRequest -Uri $archiveUrl -Headers $Headers -OutFile $archivePath
    Invoke-WebRequest -Uri $checksumsUrl -Headers $Headers -OutFile $checksumsPath

    $checksumLine = Get-Content $checksumsPath | Where-Object { $_ -match ([regex]::Escape($archive) + '$') } | Select-Object -First 1
    if (-not $checksumLine) {
        throw "checksum for $archive not found"
    }

    $expected = ($checksumLine -split '\s+')[0].ToLowerInvariant()
    $actual = (Get-FileHash -Algorithm SHA256 -Path $archivePath).Hash.ToLowerInvariant()
    if ($expected -ne $actual) {
        throw "checksum verification failed"
    }

    New-Item -ItemType Directory -Path $extractDir -Force | Out-Null
    & tar.exe -xzf $archivePath -C $extractDir

    $sourceBinary = Join-Path $extractDir "husky.exe"
    if (-not (Test-Path $sourceBinary)) {
        throw "husky.exe was not found in the release archive"
    }

    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    $targetBinary = Join-Path $InstallDir "husky.exe"
    Copy-Item -Path $sourceBinary -Destination $targetBinary -Force

    $pathUpdated = $false
    if ($AddToPath) {
        $pathUpdated = Add-InstallDirToPath -PathToAdd $InstallDir
    }

    Write-Host "Installed husky to $targetBinary"
    if ($AddToPath) {
        if ($pathUpdated) {
            Write-Host "Added $InstallDir to the user PATH. Open a new PowerShell window to use 'husky' everywhere."
        }
        else {
            Write-Host "$InstallDir is already on PATH."
        }
    }
    else {
        Write-Host "Add $InstallDir to PATH to run 'husky' without a full path."
    }
}
finally {
    if (Test-Path $TempDir) {
        Remove-Item -Path $TempDir -Recurse -Force
    }
}
