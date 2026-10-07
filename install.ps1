# Install the Axilio CLI on Windows - https://github.com/axilioai/cli
#
#   irm https://axilio.ai/install.ps1 | iex
#
# Downloads the latest release for your architecture, verifies its checksum,
# installs axilio.exe, and adds the install directory to your user PATH when
# neither your user nor the system PATH has it. The HTML manual is installed
# next to the binary, where `axilio help --html` finds it. Works in Windows
# PowerShell 5.1 and PowerShell 7+, and needs no administrator rights.
# Environment overrides:
#
#   VERSION       release tag to install (default: latest, e.g. $env:VERSION = "v0.12.0")
#   INSTALL_DIR   target directory (default: %LOCALAPPDATA%\Programs\axilio\bin).
#                 A relative path resolves against the current directory. A
#                 directory already on the user or system PATH leaves it untouched.

# `iex` runs a script in the caller's scope. The script block keeps this
# script's preferences, variables, and functions out of the user's session.
& {
    $ErrorActionPreference = 'Stop'
    # Windows PowerShell 5.1 renders Invoke-WebRequest progress very slowly.
    $ProgressPreference = 'SilentlyContinue'

    $Repo = 'axilioai/cli'
    $Bin = 'axilio'

    function Fail([string]$Message) {
        throw "error: $Message"
    }

    function Get-Arch {
        $arch = $null
        try {
            $arch = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString()
        } catch {
            # .NET Framework before 4.7.1 has no RuntimeInformation. A 32-bit
            # PowerShell on 64-bit Windows reports the OS in PROCESSOR_ARCHITEW6432.
            $arch = $env:PROCESSOR_ARCHITEW6432
            if (-not $arch) { $arch = $env:PROCESSOR_ARCHITECTURE }
        }
        switch ($arch) {
            { $_ -in 'X64', 'AMD64' } { return 'amd64' }
            'Arm64' { return 'arm64' }
            default { Fail "unsupported architecture: $arch (try: go install github.com/$Repo/cmd/$Bin@latest)" }
        }
    }

    # Get-FullDir resolves a path against PowerShell's current location and
    # drops trailing separators, except on a root: `C:` alone means "the
    # current directory on drive C", not `C:\`.
    function Get-FullDir([string]$Path) {
        $full = $ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($Path)
        if ($full -ne [IO.Path]::GetPathRoot($full)) { $full = $full.TrimEnd('\', '/') }
        return $full
    }

    function Test-PathEntry([string]$PathList, [string]$Dir) {
        foreach ($entry in ($PathList -split ';')) {
            if (-not $entry) { continue }
            $expanded = [Environment]::ExpandEnvironmentVariables($entry)
            try { $expanded = Get-FullDir $expanded } catch { $expanded = $expanded.TrimEnd('\') }
            if ($expanded -ieq $Dir) { return $true }
        }
        return $false
    }

    # Add-UserPath appends $Dir to the user PATH. It edits the registry value
    # directly, without expanding it, so entries like %USERPROFILE%\bin keep
    # their variable form and the value stays REG_EXPAND_SZ.
    function Add-UserPath([string]$Dir) {
        $key = Get-Item -LiteralPath 'HKCU:\Environment'
        $current = $key.GetValue('Path', '', 'DoNotExpandEnvironmentNames')
        if (Test-PathEntry $current $Dir) { return $false }
        $entries = @($current -split ';' | Where-Object { $_ }) + $Dir
        Set-ItemProperty -LiteralPath 'HKCU:\Environment' -Name 'Path' -Type ExpandString -Value ($entries -join ';')
        # A registry write alone does not tell running programs (Explorer, new
        # terminals) that the environment changed. SetEnvironmentVariable
        # broadcasts WM_SETTINGCHANGE, so set and remove a throwaway variable.
        $dummy = 'axilio-install-' + [Guid]::NewGuid().ToString('N')
        [Environment]::SetEnvironmentVariable($dummy, '1', 'User')
        [Environment]::SetEnvironmentVariable($dummy, [NullString]::Value, 'User')
        return $true
    }

    if ([Environment]::OSVersion.Platform -ne [PlatformID]::Win32NT) {
        Fail 'install.ps1 is for Windows; on macOS or Linux run: curl -fsSL https://axilio.ai/install.sh | sh'
    }
    # GitHub serves TLS 1.2+. Older .NET Framework defaults may not offer it.
    [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

    $arch = Get-Arch

    # --- resolve version ----------------------------------------------------
    $version = $env:VERSION
    if (-not $version) {
        try {
            $version = (Invoke-RestMethod -UseBasicParsing -Uri "https://api.github.com/repos/$Repo/releases/latest").tag_name
        } catch {
            Fail "could not determine the latest release: $($_.Exception.Message)"
        }
    }
    if (-not $version) { Fail 'could not determine the latest release' }

    $num = $version.TrimStart('v')
    $tag = "v$num"
    $archive = "${Bin}_${num}_windows_${arch}.zip"
    $base = "https://github.com/$Repo/releases/download/$tag"

    $tmp = Join-Path ([IO.Path]::GetTempPath()) ('axilio-install-' + [Guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $tmp | Out-Null
    try {
        Write-Host "Downloading $Bin $tag (windows/$arch)..."
        $archivePath = Join-Path $tmp $archive
        try {
            Invoke-WebRequest -UseBasicParsing -Uri "$base/$archive" -OutFile $archivePath
        } catch {
            Fail "download failed: $base/$archive"
        }

        # --- verify checksum (always; every release publishes checksums.txt) --
        $checksumsPath = Join-Path $tmp 'checksums.txt'
        try {
            Invoke-WebRequest -UseBasicParsing -Uri "$base/checksums.txt" -OutFile $checksumsPath
        } catch {
            Fail "could not download $base/checksums.txt; refusing to install unverified bytes"
        }
        $want = $null
        foreach ($line in Get-Content -LiteralPath $checksumsPath) {
            $fields = -split $line
            if ($fields.Count -eq 2 -and $fields[1] -eq $archive) { $want = $fields[0].ToLowerInvariant() }
        }
        if (-not $want) { Fail "no checksum listed for $archive" }
        $got = (Get-FileHash -LiteralPath $archivePath -Algorithm SHA256).Hash.ToLowerInvariant()
        if ($got -ne $want) { Fail "checksum mismatch for $archive" }

        $extracted = Join-Path $tmp 'extracted'
        Expand-Archive -LiteralPath $archivePath -DestinationPath $extracted
        $exeSource = Join-Path $extracted "$Bin.exe"
        if (-not (Test-Path -LiteralPath $exeSource)) { Fail "$Bin.exe not found in the archive" }

        # --- install --------------------------------------------------------
        $dir = $env:INSTALL_DIR
        if (-not $dir) { $dir = Join-Path $env:LOCALAPPDATA "Programs\$Bin\bin" }
        $dir = Get-FullDir $dir
        try {
            New-Item -ItemType Directory -Force -Path $dir | Out-Null
        } catch {
            Fail "cannot create $dir; set INSTALL_DIR to a writable path"
        }
        $exeTarget = Join-Path $dir "$Bin.exe"
        # Windows cannot overwrite a running executable but can rename one, so
        # an existing axilio.exe is moved aside first (this also lets a running
        # `axilio runs watch` keep going). A renamed binary that is still
        # running cannot be deleted, so each install moves it to a fresh name
        # and sweeps the leftovers it can, including the `.axilio.exe.old`
        # that `axilio upgrade` leaves behind.
        Get-ChildItem -LiteralPath $dir -Filter ".$Bin.exe*.old" -Force -ErrorAction SilentlyContinue |
            ForEach-Object { Remove-Item -LiteralPath $_.FullName -Force -ErrorAction SilentlyContinue }
        $old = Join-Path $dir (".$Bin.exe." + [Guid]::NewGuid().ToString('N') + '.old')
        if (Test-Path -LiteralPath $exeTarget) {
            try {
                Move-Item -LiteralPath $exeTarget -Destination $old -Force
            } catch {
                Fail "cannot replace $exeTarget; close running axilio processes or set INSTALL_DIR"
            }
        }
        try {
            Copy-Item -LiteralPath $exeSource -Destination $exeTarget -Force
        } catch {
            if (Test-Path -LiteralPath $old) { Move-Item -LiteralPath $old -Destination $exeTarget -Force }
            Fail "cannot write to $dir; set INSTALL_DIR to a writable path"
        }
        Remove-Item -LiteralPath $old -Force -ErrorAction SilentlyContinue
        Write-Host "Installed $Bin $tag to $exeTarget"

        # --- HTML manual (best effort; never undoes the installed binary) ----
        $htmlSource = Join-Path $extracted "man\$Bin.1.html"
        if (Test-Path -LiteralPath $htmlSource) {
            try {
                $manDir = Join-Path $dir 'man'
                New-Item -ItemType Directory -Force -Path $manDir | Out-Null
                Copy-Item -LiteralPath $htmlSource -Destination (Join-Path $manDir "$Bin.1.html") -Force
                Write-Host "Installed HTML manual to $manDir\$Bin.1.html"
            } catch {
                Write-Warning "cannot install the HTML manual: $($_.Exception.Message); the binary remains installed"
            }
        } else {
            Write-Warning "$archive does not contain man\$Bin.1.html; the binary is installed without the HTML manual"
        }

        # --- PATH -------------------------------------------------------------
        # Persist based on the saved user and system PATH, not this process's
        # PATH, which a launcher may have extended for this session only.
        if (-not (Test-PathEntry ([Environment]::GetEnvironmentVariable('Path', 'Machine')) $dir)) {
            if (Add-UserPath $dir) {
                Write-Host "Added $dir to your user PATH. New terminals pick it up."
            }
        }
        if (-not (Test-PathEntry $env:Path $dir)) { $env:Path = "$env:Path;$dir" }

        Write-Host 'For PowerShell tab completion, add this line to your $PROFILE:'
        Write-Host '  axilio completion powershell | Out-String | Invoke-Expression'

        & $exeTarget --version
    } finally {
        Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue
    }
}
