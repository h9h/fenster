<#
.SYNOPSIS
    Builds fenster, stops the running copy, installs the new binary and starts it.

.DESCRIPTION
    Deployment has to stop the running instance first: Windows locks a
    running executable, so the copy would fail outright.

    The instance is stopped with `fenster.exe -quit`, which posts WM_CLOSE to
    the running instance's hidden window — the same message the tray menu's
    "Beenden" sends. It is deliberately not Stop-Process: terminating fenster
    leaves its icon behind in the notification area until the shell next
    reaps it, which looks like a crash.

    The script builds before deploying, so what is installed is always what
    the working tree currently says.

.PARAMETER Destination
    Directory to install into. Defaults to C:\Tools.

.PARAMETER StopTimeoutSeconds
    How long to wait for the running instance to exit before giving up. The
    script aborts rather than overwriting a binary that is still running.

.EXAMPLE
    .\deploy.ps1

.EXAMPLE
    .\deploy.ps1 -Destination D:\Apps
#>
[CmdletBinding()]
param(
    [string] $Destination = 'C:\Tools',
    [int]    $StopTimeoutSeconds = 10
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$repo = $PSScriptRoot
$built = Join-Path $repo 'fenster.exe'
$target = Join-Path $Destination 'fenster.exe'

Push-Location $repo
try {
    Write-Host "Building $built ..."
    # The same flags the README documents: no console window, stripped.
    & go build '-ldflags=-H=windowsgui -s -w' -o $built ./cmd/fenster
    if ($LASTEXITCODE -ne 0) {
        throw "go build failed (exit code $LASTEXITCODE)"
    }

    # Start-Process -Wait, not "& $built -quit": a -H=windowsgui binary
    # detaches immediately when invoked directly, so the call would return
    # before the signal was sent and $LASTEXITCODE would mean nothing.
    Write-Host 'Stopping a running instance ...'
    $quit = Start-Process -FilePath $built -ArgumentList '-quit' -Wait -PassThru
    if ($quit.ExitCode -eq 0) {
        # The old instance is shutting down. Wait for it to actually go:
        # the signal is asynchronous, and copying over a still-running
        # binary fails.
        $deadline = (Get-Date).AddSeconds($StopTimeoutSeconds)
        while (Get-Process -Name 'fenster' -ErrorAction SilentlyContinue) {
            if ((Get-Date) -gt $deadline) {
                throw "fenster was asked to quit but is still running after $StopTimeoutSeconds s. Aborting rather than overwriting a running binary."
            }
            Start-Sleep -Milliseconds 200
        }
        Write-Host '  stopped.'
    }
    else {
        Write-Host '  nothing was running.'
    }

    if (-not (Test-Path -LiteralPath $Destination)) {
        New-Item -ItemType Directory -Path $Destination -Force | Out-Null
    }

    Write-Host "Installing to $target ..."
    Copy-Item -LiteralPath $built -Destination $target -Force

    Write-Host "Starting $target ..."
    Start-Process -FilePath $target

    Write-Host 'Deployed.'
}
finally {
    Pop-Location
}
