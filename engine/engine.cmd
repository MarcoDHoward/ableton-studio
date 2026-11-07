@echo off
rem engine: run the music engine. Uses the project's .venv if present, otherwise the Python on PATH.
if exist "%~dp0.venv\Scripts\python.exe" (
  set "PYTHONPATH=%~dp0;%PYTHONPATH%" & "%~dp0.venv\Scripts\python.exe" -m music_engine %*
) else (
  set "PYTHONPATH=%~dp0;%PYTHONPATH%" & python -m music_engine %*
)
