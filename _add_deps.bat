@echo off
cd /d C:\Users\goldf\rasa
C:\PROGRA~1\Go\bin\go.exe get github.com/google/uuid@latest
C:\PROGRA~1\Go\bin\go.exe get github.com/redis/go-redis/v9@latest
C:\PROGRA~1\Go\bin\go.exe mod tidy
