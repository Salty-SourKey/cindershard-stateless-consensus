#Requires -Version 5.1

# --- Configuration ---
# 설정: 이 값들은 deploy.ps1 스크립트와 일치해야 합니다.
$ClientPidFile = "client.pid" # 클라이언트 PID 파일 이름 (사용되지 않을 수 있음)
$ServerPidFile = "server.pid" # 서버 마커 파일 이름
# deploy.ps1에서 사용된 실행 파일 이름 (확장자 제외). Get-Process는 보통 확장자 없이 이름을 사용합니다.
$ClientExeName = "client"     # 클라이언트 실행 파일 이름 (Client executable name (without extension))
$ServerExeName = "main"       # 서버 실행 파일 이름 (Server executable name (without extension))

# --- Function to Stop Processes by Name ---
# 이름을 기준으로 프로세스를 중지하는 함수
function Stop-ProcessesByName {
    param(
        [Parameter(Mandatory=$true)]
        [string]$ProcessName,

        [Parameter(Mandatory=$true)]
        [string]$ProcessType # "Client" or "Server" for messages (메시지용 "Client" 또는 "Server")
    )

    Write-Host "Attempting to stop $ProcessType processes by name: '$ProcessName'..."
    try {
        # Get processes by name, handle case where none are found gracefully
        # 이름으로 프로세스 가져오기, 찾지 못한 경우 정상적으로 처리
        $processes = Get-Process -Name $ProcessName -ErrorAction SilentlyContinue

        if ($null -eq $processes) {
            Write-Host "No running $ProcessType processes found with name '$ProcessName'." -ForegroundColor Yellow
            return
        }

        # Stop the found processes
        # 찾은 프로세스 중지 (-Force 옵션으로 강제 종료 시도)
        $processes | Stop-Process -Force -ErrorAction SilentlyContinue # Continue even if some fail (일부 실패해도 계속 진행)

        # Report which PIDs were targeted for stopping
        # 어떤 PID가 중지 대상으로 지정되었는지 보고
        $pidsTargeted = ($processes | Select-Object -ExpandProperty Id) -join ', '
        Write-Host "Stop command issued for $ProcessType processes named '$ProcessName' with PIDs: $pidsTargeted" -ForegroundColor Green

    } catch {
        Write-Error "An error occurred while trying to stop $ProcessType processes named '$ProcessName'. Error: $($_.Exception.Message)"
    }
}


# --- Stop Client Processes ---
# 클라이언트 프로세스 중지
# deploy.ps1이 client.exe를 시작하고 PID를 client.pid에 저장했다면 아래 주석 해제
# Stop-ProcessesFromPidFile -PidFilePath $ClientPidFile -ProcessType "Client"
# 현재는 이름 기반으로 클라이언트 프로세스 중지 시도
Stop-ProcessesByName -ProcessName $ClientExeName -ProcessType "Client"


# --- Stop Server Processes ---
# 서버 프로세스 중지
# deploy.ps1은 PID를 저장하지 않으므로 이름 기반으로 서버 프로세스 중지
Stop-ProcessesByName -ProcessName $ServerExeName -ProcessType "Server"

# --- Clean up Server Marker File ---
# 서버 마커 파일 정리
# deploy.ps1이 생성한 서버 마커 파일을 항상 제거 시도
if (Test-Path -Path $ServerPidFile -PathType Leaf) {
    Write-Host "Removing server marker file: $ServerPidFile"
    try {
        Remove-Item -Path $ServerPidFile -Force -ErrorAction Stop
        Write-Host "Server marker file '$ServerPidFile' removed." -ForegroundColor Green
    } catch {
        Write-Error "Failed to remove server marker file '$ServerPidFile'. Error: $($_.Exception.Message)"
    }
} else {
    # 마커 파일이 없는 경우는 이미 서버가 중지되었거나 시작되지 않은 상태일 수 있음
    Write-Host "Server marker file '$ServerPidFile' not found (perhaps already stopped or not started)." -ForegroundColor Yellow
}

Write-Host "Stop script finished."
exit 0
