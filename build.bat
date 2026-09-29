@echo off
chcp 65001 >nul
echo ========================================
echo   OCS 超星题库 - EXE 构建脚本
echo ========================================
echo.

if not exist "venv" (
    echo [1/4] 创建虚拟环境...
    python -m venv venv
    if %errorlevel% neq 0 (
        echo ❌ 创建虚拟环境失败
        pause
        exit /b 1
    )
)

echo [2/4] 安装依赖...
call .\venv\Scripts\python.exe -m pip install -r requirements.txt --quiet
if %errorlevel% neq 0 (
    echo ❌ 依赖安装失败
    pause
    exit /b 1
)

call .\venv\Scripts\python.exe -m pip install pyinstaller --quiet

echo [3/4] 构建 EXE...
call .\venv\Scripts\pyinstaller build.spec --distpath dist --workpath build --clean
if %errorlevel% neq 0 (
    echo ❌ 构建失败
    pause
    exit /b 1
)

echo [4/4] 复制配置文件...
copy /Y config.yaml dist\config.yaml >nul

echo.
echo ========================================
echo   ✅ 构建完成！
echo   输出: dist\ocs-tiku.exe
echo ========================================
pause
