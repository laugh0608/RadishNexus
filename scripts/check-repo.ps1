#!/usr/bin/env pwsh
$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

$scriptDirectory = Split-Path -Parent $MyInvocation.MyCommand.Path
& python3 (Join-Path $scriptDirectory "check_repo.py") @args
exit $LASTEXITCODE
