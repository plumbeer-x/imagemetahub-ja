@echo off
rem ソースからパッチャーをビルドする（Go 1.24以上が必要）
cd /d %~dp0
go build -ldflags="-s -w" -o ..\imagemetahub-ja-patch.exe .
echo.
echo ビルド完了: ..\imagemetahub-ja-patch.exe
pause
