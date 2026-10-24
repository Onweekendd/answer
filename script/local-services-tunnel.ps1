# Licensed to the Apache Software Foundation (ASF) under one
# or more contributor license agreements. See the NOTICE file distributed
# with this work for additional information regarding copyright ownership.
# The ASF licenses this file to You under the Apache License, Version 2.0
# (the "License"); you may not use this file except in compliance with
# the License. You may obtain a copy of the License at
# http://www.apache.org/licenses/LICENSE-2.0
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

param(
    [ValidatePattern('^[A-Za-z0-9_.@:-]+$')]
    [string]$RemoteHost = 'screeps-server',
    [int]$HubPort = 8765,
    [int]$CubeApiPort = 3000,
    [int]$CubeProxyPort = 443
)

$ErrorActionPreference = 'Stop'
$taskRoot = Split-Path -Parent $PSScriptRoot
$stateDirectory = Join-Path $taskRoot 'tmp'
[System.IO.Directory]::CreateDirectory($stateDirectory) | Out-Null
$lockPath = Join-Path $stateDirectory 'local-services-tunnel.lock'
$statePath = Join-Path $stateDirectory 'local-services-tunnel.json'
$logPath = Join-Path $stateDirectory 'local-services-tunnel.stderr.log'
$lock = [System.IO.File]::Open($lockPath, 'OpenOrCreate', 'ReadWrite', 'None')
$sshProcess = $null
$sshPath = (Get-Command ssh.exe -ErrorAction Stop).Source

try {
    while ($true) {
        $sshArguments = @(
            '-N', '-T', '-o', 'BatchMode=yes', '-o', 'StrictHostKeyChecking=yes',
            '-o', 'ExitOnForwardFailure=yes', '-o', 'ConnectTimeout=10',
            '-o', 'ServerAliveInterval=30', '-o', 'ServerAliveCountMax=3',
            '-R', "127.0.0.1:28765:127.0.0.1:$HubPort",
            '-R', "127.0.0.1:23000:127.0.0.1:$CubeApiPort",
            '-R', "127.0.0.1:23443:127.0.0.1:$CubeProxyPort", $RemoteHost
        )
        $sshProcess = Start-Process -FilePath $sshPath -ArgumentList $sshArguments `
            -WindowStyle Hidden -PassThru -RedirectStandardError $logPath
        @{ managerPid = $PID; sshPid = $sshProcess.Id; remoteHost = $RemoteHost } |
            ConvertTo-Json | Set-Content -LiteralPath $statePath -Encoding UTF8
        $sshProcess.WaitForExit()
        Start-Sleep -Seconds 5
    }
}
finally {
    if ($sshProcess -and -not $sshProcess.HasExited) { $sshProcess.Kill() }
    $lock.Dispose()
    Remove-Item -LiteralPath $statePath -Force -ErrorAction SilentlyContinue
}
