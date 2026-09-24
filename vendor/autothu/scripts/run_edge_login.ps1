# 在 Windows 侧启动 Edge (CDP) 并运行登录脚本，供 WSL 调用:
#   powershell.exe -File scripts/run_edge_login.ps1
$ErrorActionPreference = "Stop"
$Edge = "C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe"
$Profile = "$env:TEMP\autothu-edge-cdp"
$Py = "C:\Users\lenovo\AppData\Local\Programs\Python\Python313\python.exe"
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$LoginScript = Join-Path $ScriptDir "edge_login_winhost.py"

# WSL 项目路径
$WslScript = "\\wsl.localhost\Ubuntu\home\lenovo\autoTHU\AutoThu\scripts\edge_login_winhost.py"
if (Test-Path $WslScript) { $LoginScript = $WslScript }

New-Item -ItemType Directory -Force -Path $Profile | Out-Null

# 若 9222 未监听则启动 Edge
$listening = $false
try {
  $r = Invoke-WebRequest -Uri "http://127.0.0.1:9222/json/version" -UseBasicParsing -TimeoutSec 2
  $listening = $true
} catch {}

if (-not $listening) {
  Write-Host "启动 Edge (remote-debugging-port=9222)..."
  Start-Process -FilePath $Edge -ArgumentList @(
    "--remote-debugging-port=9222",
    "--user-data-dir=$Profile",
    "--no-first-run",
    "https://learn.tsinghua.edu.cn/f/login"
  )
  Start-Sleep -Seconds 5
}

$env:AUTOTHU_SESSION = "\\wsl.localhost\Ubuntu\home\lenovo\.config\autothu\session.json"
Write-Host "运行登录脚本: $LoginScript"
& $Py $LoginScript
exit $LASTEXITCODE
