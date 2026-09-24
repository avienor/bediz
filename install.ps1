# Installs Bediz from a GitHub Release on Windows amd64:
#
#   irm https://raw.githubusercontent.com/avienor/bediz/master/install.ps1 | iex
#
# It installs the latest release, or the tag in $env:BEDIZ_VERSION, verifies
# the archive against the release's SHA256SUMS, and copies bediz.exe to
# $env:BEDIZ_INSTALL_DIR (default $env:LOCALAPPDATA\Programs\bediz). It then
# installs the agent skill from the same tag for the user with npx when
# Node.js is available. It never changes Path or needs administrator rights.
# Errors are thrown instead of exiting, so `iex` keeps the caller's session.

function Install-Bediz {
    $ErrorActionPreference = 'Stop'
    $ProgressPreference = 'SilentlyContinue'
    [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
    $repo = 'avienor/bediz'

    $arch = $env:PROCESSOR_ARCHITECTURE
    if ($env:PROCESSOR_ARCHITEW6432) { $arch = $env:PROCESSOR_ARCHITEW6432 }
    if ($arch -ne 'AMD64') {
        throw "bediz install: no release for Windows $arch; releases exist for Linux amd64, macOS arm64 (Apple silicon), and Windows amd64"
    }

    $tag = $env:BEDIZ_VERSION
    if (-not $tag) {
        try {
            $tag = (Invoke-RestMethod "https://api.github.com/repos/$repo/releases/latest" -UseBasicParsing).tag_name
        } catch {
            throw "bediz install: no stable release is published yet, or GitHub is unreachable; choose a tag from https://github.com/$repo/releases and set `$env:BEDIZ_VERSION"
        }
    }
    $dir = $env:BEDIZ_INSTALL_DIR
    if (-not $dir) { $dir = Join-Path $env:LOCALAPPDATA 'Programs\bediz' }
    $dir = [IO.Path]::GetFullPath($dir)
    $archive = "bediz_${tag}_windows_amd64.zip"

    $tmp = Join-Path ([IO.Path]::GetTempPath()) ([IO.Path]::GetRandomFileName())
    New-Item -ItemType Directory $tmp | Out-Null
    try {
        Write-Host "Downloading Bediz $tag for windows_amd64"
        $base = "https://github.com/$repo/releases/download/$tag"
        foreach ($name in @($archive, 'SHA256SUMS')) {
            try {
                Invoke-WebRequest "$base/$name" -OutFile (Join-Path $tmp $name) -UseBasicParsing
            } catch {
                throw "bediz install: could not download $base/$name"
            }
        }

        $line = Get-Content (Join-Path $tmp 'SHA256SUMS') | Where-Object { $_ -match "^([0-9a-f]{64})  $([regex]::Escape($archive))$" } | Select-Object -First 1
        if (-not $line) { throw "bediz install: SHA256SUMS has no line for $archive; nothing was installed" }
        $want = $line.Substring(0, 64)
        $got = (Get-FileHash -Algorithm SHA256 (Join-Path $tmp $archive)).Hash
        if ($got -ne $want) { throw "bediz install: the checksum of $archive did not verify; nothing was installed" }

        Expand-Archive (Join-Path $tmp $archive) -DestinationPath (Join-Path $tmp 'x')
        New-Item -ItemType Directory -Force $dir | Out-Null
        $exe = Join-Path $dir 'bediz.exe'
        Copy-Item (Join-Path $tmp 'x\bediz.exe') $exe -Force
    } finally {
        Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
    }

    if ((& $exe version --json) -notmatch ('"version":"' + [regex]::Escape($tag) + '"')) {
        throw "bediz install: $exe does not report version $tag"
    }
    Write-Host "Installed $exe"

    $onPath = ($env:Path -split ';') | Where-Object { $_ -and ([IO.Path]::GetFullPath($_).TrimEnd('\') -eq $dir.TrimEnd('\')) }
    if ($onPath) {
        $found = (Get-Command bediz -ErrorAction SilentlyContinue).Source
        if ($found -and $found -ne $exe) {
            Write-Host "Note: bediz resolves to $found, which comes before $dir on Path"
        }
    } else {
        Write-Host "Note: $dir is not on Path; add it to your user Path, for example:"
        Write-Host "  [Environment]::SetEnvironmentVariable('Path', [Environment]::GetEnvironmentVariable('Path', 'User') + ';$dir', 'User')"
    }

    $skill = "https://github.com/$repo/tree/$tag/skills/bediz"
    if (Get-Command npx.cmd -ErrorAction SilentlyContinue) {
        Write-Host "Installing the Bediz agent skill from $tag"
        & npx.cmd -y skills add $skill -g -y
        if ($LASTEXITCODE -eq 0) {
            Write-Host 'Installed the agent skill; it loads in a new agent session'
        } else {
            Write-Host "Note: the agent skill was not installed; retry with: npx skills add $skill -g"
        }
    } else {
        Write-Host "Note: the agent skill needs Node.js; after installing it, run: npx skills add $skill -g"
    }
}

Install-Bediz
