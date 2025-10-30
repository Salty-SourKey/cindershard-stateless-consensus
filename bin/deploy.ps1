#Requires -Version 5.1 # 스크립트 실행에 필요한 최소 PowerShell 버전을 명시합니다.

# --- Configuration ---
# 설정: 필요에 따라 이 값들을 수정하세요.
$ServerPidFile = "server.pid"          # 서버 실행 여부를 표시하는 마커 파일 이름 (Marker file name indicating server execution status)
$ConfigFile = ".\deploy\config.conf"   # 설정 파일 경로 (Configuration file path)
$MainExe = ".\main.exe"                # Go로 빌드될 주 실행 파일 이름 (Main executable file name to be built by Go)
$ClientExe = ".\client.exe"            # Go로 빌드될 클라이언트 실행 파일 이름 (Client executable file name to be built by Go)
$MainSource = "../main/"               # 주 실행 파일의 Go 소스 디렉토리 (Go source directory for the main executable)
$ClientSource = "../client/"           # 클라이언트 실행 파일의 Go 소스 디렉토리 (Go source directory for the client executable)
$ErrorLogPath = "."                    # 로그 파일 저장 경로 (Path to save log files)

# --- Function Definitions ---
# Helper function to safely convert input to integer
# 입력 값을 정수로 안전하게 변환하는 헬퍼 함수
function ConvertTo-Int ($value, $defaultValue = $null) {
    try {
        # Attempt to parse the value as an integer
        # 값을 정수로 파싱 시도
        return [int]::Parse($value)
    } catch {
        # Handle parsing failure
        # 파싱 실패 처리
        if ($defaultValue -ne $null) {
            Write-Warning "Could not parse '$value' as integer, using default '$defaultValue'."
            return $defaultValue
        } else {
            Write-Error "Could not parse '$value' as integer."
            # Re-throw the exception to be caught by the main script logic
            # 주 스크립트 로직에서 잡을 수 있도록 예외를 다시 던짐
            throw $_
        }
    }
}

# --- Check if already running ---
# Check if the server is already running based on the marker file's existence
# 마커 파일 존재 여부를 기준으로 서버가 이미 실행 중인지 확인
Write-Host "Checking for existing marker file: $ServerPidFile"
if (Test-Path -Path $ServerPidFile -PathType Leaf) {
    Write-Host "Servers appear to be running (marker file '$ServerPidFile' found)." -ForegroundColor Yellow
    Write-Host "To force restart, delete '$ServerPidFile' and run this script again."
    exit 0 # Exit successfully as servers are already running (서버가 이미 실행 중이므로 성공적으로 종료)
}

# --- Check for config file ---
# Verify the configuration file exists
# 설정 파일 존재 확인
Write-Host "Checking for configuration file: $ConfigFile"
if (-not (Test-Path -Path $ConfigFile -PathType Leaf)) {
    Write-Error "ERROR: Configuration file not found: $ConfigFile"
    exit 1 # Exit with error code 1 (오류 코드 1로 종료)
}

# --- Read configuration ---
# Read SHARD and COMMITTEE values from the config file
# 설정 파일에서 SHARD 및 COMMITTEE 값 읽기
Write-Host "Reading configuration from $ConfigFile..."
$Shard = $null
$Committee = $null
try {
    # Get the first two lines of the config file
    # 설정 파일의 첫 두 줄 가져오기
    $configLines = Get-Content $ConfigFile -TotalCount 2 -ErrorAction Stop

    # Process the first line for SHARD value
    # 첫 번째 줄에서 SHARD 값 처리
    if ($configLines.Count -ge 1) {
        $parts = $configLines[0] -split ':\s+', 2 # Split by ": " allowing variable whitespace (다양한 공백을 허용하며 ": " 기준으로 분리)
        if ($parts.Count -eq 2) {
            $Shard = $parts[1].Trim() # Trim whitespace from the value (값에서 공백 제거)
            Write-Host "  Found Shard raw value: '$Shard'"
        }
    }
    # Process the second line for COMMITTEE value
    # 두 번째 줄에서 COMMITTEE 값 처리
    if ($configLines.Count -ge 2) {
        $parts = $configLines[1] -split ':\s+', 2
        if ($parts.Count -eq 2) {
            $Committee = $parts[1].Trim()
            Write-Host "  Found Committee raw value: '$Committee'"
        }
    }
} catch {
    Write-Error "ERROR: Failed to read or parse configuration file '$ConfigFile'. Error: $($_.Exception.Message)"
    exit 1
}

# --- Validate configuration ---
# Validate that SHARD and COMMITTEE were read and are valid non-negative integers
# SHARD와 COMMITTEE가 읽혔고 유효한 음이 아닌 정수인지 확인
Write-Host "Validating configuration..."
if ($null -eq $Shard) {
    Write-Error "ERROR: Failed to read SHARD value from $ConfigFile"
    exit 1
}
if ($null -eq $Committee) {
    Write-Error "ERROR: Failed to read COMMITTEE value from $ConfigFile"
    exit 1
}

# Convert to integers and perform validation
# 정수로 변환하고 유효성 검사 수행
try {
    $ShardInt = ConvertTo-Int $Shard
    $CommitteeInt = ConvertTo-Int $Committee

    if ($ShardInt -lt 0) {
        Write-Error "ERROR: SHARD value '$ShardInt' cannot be negative."
        exit 1
    }
    if ($CommitteeInt -lt 0) {
        Write-Error "ERROR: COMMITTEE value '$CommitteeInt' cannot be negative."
        exit 1
    }
} catch {
    # Error already written by ConvertTo-Int or the catch block here handles the re-thrown exception
    # ConvertTo-Int에서 오류가 이미 기록되었거나 여기서 catch 블록이 다시 던져진 예외를 처리함
    Write-Error "ERROR: Configuration values SHARD ('$Shard') or COMMITTEE ('$Committee') are not valid integers."
    exit 1
}

Write-Host "Configuration read successfully: Shard=$ShardInt, Committee=$CommitteeInt" -ForegroundColor Green

# --- Loop condition checks ---
# Check conditions before starting loops
# 루프 시작 전 조건 확인
if ($ShardInt -lt 1) {
    Write-Host "INFO: SHARD value is $ShardInt. No shard-specific processes (Block Builders/Nodes) will be started." -ForegroundColor Yellow
}
if ($CommitteeInt -lt 1) {
    Write-Host "INFO: COMMITTEE value is $CommitteeInt. No Node processes will be started." -ForegroundColor Yellow
}

# --- Prepare environment ---
Write-Host "Preparing environment..."
Write-Host "Deleting old log files (*.log) in '$ErrorLogPath'..."
# Remove old log files, suppressing errors if no files match
# 이전 로그 파일 삭제, 일치하는 파일이 없으면 오류 표시 안 함
Remove-Item -Path (Join-Path -Path $ErrorLogPath -ChildPath "*.log") -ErrorAction SilentlyContinue

# --- Build Go applications ---
Write-Host "Building Go applications..."
$buildSuccess = $true
try {
    # Build the main application
    # 주 애플리케이션 빌드
    Write-Host "Building main application ($MainExe)..."
    & go build -o $MainExe $MainSource
    if ($LASTEXITCODE -ne 0) {
        Write-Error "ERROR: Failed to build main application from $MainSource. Exit code: $LASTEXITCODE"
        $buildSuccess = $false
    }

    # Build the client application only if the main build succeeded
    # 주 빌드가 성공한 경우에만 클라이언트 애플리케이션 빌드
    if ($buildSuccess) {
        Write-Host "Building client application ($ClientExe)..."
        & go build -o $ClientExe $ClientSource
        if ($LASTEXITCODE -ne 0) {
            Write-Error "ERROR: Failed to build client application from $ClientSource. Exit code: $LASTEXITCODE"
            $buildSuccess = $false
        }
    }
} catch {
    Write-Error "ERROR: An exception occurred during the build process: $($_.Exception.Message)"
    $buildSuccess = $false
}

# Exit if the build process failed
# 빌드 프로세스가 실패하면 종료
if (-not $buildSuccess) {
    Write-Error "Build failed. Exiting script."
    # Attempt cleanup of marker file if it was somehow created before build failure
    # 빌드 실패 전에 마커 파일이 생성된 경우 정리 시도
    if (Test-Path -Path $ServerPidFile -PathType Leaf) {
        Write-Host "Attempting to remove marker file '$ServerPidFile' due to build failure..."
        Remove-Item -Path $ServerPidFile -Force -ErrorAction SilentlyContinue
    }
    exit 1
}
Write-Host "Build successful." -ForegroundColor Green


# --- Create marker file ---
# Create the marker file to indicate servers are starting/running
# 서버 시작/실행을 나타내는 마커 파일 생성
Write-Host "Creating server marker file: $ServerPidFile"
try {
    # Create an empty file, overwriting if it somehow exists
    # 빈 파일 생성, 존재할 경우 덮어쓰기
    New-Item -Path $ServerPidFile -ItemType File -Force -ErrorAction Stop | Out-Null
} catch {
    Write-Error "ERROR: Failed to create marker file '$ServerPidFile'. Check permissions. Error: $($_.Exception.Message)"
    exit 1
}

# --- Start server processes ---
Write-Host "Starting server processes..."

# Define common arguments for Start-Process to keep code DRY
# 코드 중복을 피하기 위해 Start-Process 공통 인수 정의
$commonProcessArgs = @{
    FilePath = $MainExe          # Path to the executable (실행 파일 경로)
    WindowStyle = 'Minimized'    # Start the process minimized (프로세스를 최소화된 상태로 시작)
    PassThru = $false            # Do not return the process object (프로세스 객체를 반환하지 않음)
}

# Start blockbuilders (only if SHARD >= 1)
# 블록 빌더 시작 (SHARD >= 1인 경우에만)
if ($ShardInt -ge 1) {
    Write-Host "Starting Block Builders (Shards 0 to $($ShardInt - 1))..."
    # Loop from 0 up to (but not including) ShardInt
    # 0부터 ShardInt-1까지 루프
    for ($i = 0; $i -lt $ShardInt; $i++) {
        Write-Host "  Starting Block Builder for Shard $i..."
        $logFile = Join-Path -Path $ErrorLogPath -ChildPath "errorBB$i.log"
        $arguments = "-sim=false -mode=blockbuilder -shard=$i -id=0"
        try {
            # Start the process using splatting for parameters
            # 스플래팅을 사용하여 프로세스 시작
            Start-Process @commonProcessArgs -ArgumentList $arguments -RedirectStandardError $logFile -ErrorAction Stop
            Write-Host "    Started process, logging stderr to $logFile"
            Start-Sleep -Seconds 2 # Pause for 2 seconds (2초간 일시 중지)
        } catch {
            Write-Error "ERROR: Failed to start Block Builder for Shard $i. Error: $($_.Exception.Message)"
            # Consider adding logic here to stop already started processes or just exit
            # 이미 시작된 프로세스를 중지하거나 종료하는 로직 추가 고려
        }
    }
}

# Start gateway
# 게이트웨이 시작
Write-Host "Starting Gateway..."
$logFile = Join-Path -Path $ErrorLogPath -ChildPath "errorGate.log"
$arguments = "-sim=false -mode=gateway -shard=0 -id=0"
try {
    Start-Process @commonProcessArgs -ArgumentList $arguments -RedirectStandardError $logFile -ErrorAction Stop
    Write-Host "  Started process, logging stderr to $logFile"
    Start-Sleep -Seconds 1 # Pause for 1 second (1초간 일시 중지)
} catch {
    Write-Error "ERROR: Failed to start Gateway. Error: $($_.Exception.Message)"
    # Consider adding logic here to stop already started processes or just exit
}

# Start nodes (only if SHARD >= 1 and COMMITTEE >= 1)
# 노드 시작 (SHARD >= 1이고 COMMITTEE >= 1인 경우에만)
if (($ShardInt -ge 1) -and ($CommitteeInt -ge 1)) {
    Write-Host "Starting Nodes (Shards 0 to $($ShardInt - 1), IDs 1 to $CommitteeInt)..."
    # Outer loop: 0 to ShardInt-1
    # 외부 루프: 0부터 ShardInt-1까지
    for ($i = 0; $i -lt $ShardInt; $i++) {
        # Inner loop: 1 to CommitteeInt (inclusive)
        # 내부 루프: 1부터 CommitteeInt까지 (포함)
        for ($j = 1; $j -le $CommitteeInt; $j++) {
            Write-Host "  Starting Node for Shard $i, ID $j..."
            $logFile = Join-Path -Path $ErrorLogPath -ChildPath "errorShard${i}Node$j.log"
            $arguments = "-sim=false -mode=node -shard=$i -id=$j"
            try {
                Start-Process @commonProcessArgs -ArgumentList $arguments -RedirectStandardError $logFile -ErrorAction Stop
                Write-Host "    Started process, logging stderr to $logFile"
                Start-Sleep -Seconds 1 # Pause for 1 second (1초간 일시 중지)
            } catch {
                Write-Error "ERROR: Failed to start Node for Shard $i, ID $j. Error: $($_.Exception.Message)"
                # Consider adding logic here to stop already started processes or just exit
            }
        }
    }
}

# --- Final Messages ---
Write-Host "============================================================" -ForegroundColor Cyan
Write-Host " All server processes initiated." -ForegroundColor Green
Write-Host " The marker file '$ServerPidFile' has been created." -ForegroundColor Green
Write-Host " Delete this file to allow the script to run again." -ForegroundColor Yellow
Write-Host " Check individual error*.log files for any startup issues." -ForegroundColor Yellow
Write-Host "============================================================" -ForegroundColor Cyan

exit 0 # Exit with success code 0 (성공 코드 0으로 종료)

