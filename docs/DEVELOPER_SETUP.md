# Developer Setup Guide

## Prerequisites

| Component | Version | Install Command |
|-----------|---------|----------------|
| Windows 11 | 23H2+ | System Settings |
| Python | 3.12.x | `.\scripts\setup_windows.ps1` or MS Store |
| PostgreSQL | 16+ | `winget install PostgreSQL` or EDB installer |
| Redis | 7.x | `winget install Redis` |
| Ollama | latest | `ollama.com/download/windows` |
| Git | 2.40+ | `winget install Git.Git` |
| Go | 1.24+ | `winget install GoLang.Go` |

## One-Time Environment Bootstrap

```powershell
# From PowerShell
cd ~\rasa
.\scripts\setup_windows.ps1        # Installs Python, pip, deps
.\scripts\create_databases.ps1     # Creates all 6 PostgreSQL databases
.\scripts\bootstrap_schema.ps1     # Applies all migrations
```

## Daily Development Workflow

### 1. Start Services
```powershell
# Terminal 1: PostgreSQL (if not running as service)
pg_ctl start -D $env:PGDATA

# Terminal 2: Redis
redis-server

# Terminal 3: Ollama (if not running as service)
ollama serve

# Verify all are listening
netstat -an | findstr "5432 6379 11434"
```

### 2. Start RASA Services
```bash
# Start all services
honcho start

# Or start individual components
honcho start orchestrator
honcho start pool-controller
honcho start llm-gateway
```

### 3. Run Component Tests
```powershell
# Run all tests
pytest tests/ -v

# Run specific test
pytest tests/ -v -k "test_name"

# Test dispatcher
python -m rasa.agent.dispatcher --soul coder-v2-dev --goal "Hello world" --dry-run
```

### 4. Develop a New Soul / Worker
1. Create a new soul sheet in `souls/my-agent-v1.yaml` (use `souls/coder-v2-dev.yaml` as template)
2. Add it to `config/pool.yaml` under `souls:`
3. Test with: `python -m rasa.agent.dispatcher --soul my-agent-v1 --goal "Test" --dry-run`

## Debugging

### Watch the task queue live
```bash
psql -U postgres -d rasa_orch -c "SELECT id, soul_id, status, title, created_at FROM tasks ORDER BY created_at DESC LIMIT 10;"
```

### Check Ollama directly
```bash
curl http://localhost:11434/api/generate -d '{"model": "gemma4:31b-cloud", "prompt": "Hello", "stream": false}'
```

### Trace a dispatcher run
```bash
python -m rasa.agent.dispatcher --soul coder-v2-dev --goal "Test" --dry-run 2>&1
```

## Common Issues

| Symptom | Cause | Fix |
|---------|-------|-----|
| `ModuleNotFoundError` | Python packages not installed | `pip install -e ".[dev]"` |
| `Connection refused` to 5432 | PostgreSQL not running | `pg_ctl start` |
| `psql: FATAL password authentication` | PGUSER/PGPASSWORD not set | Set `RASA_DB_PASSWORD` env var |
| Dispatcher returns empty JSON | Ollama not responding | `ollama serve` |
| `go build` fails | Go module cache stale | `go mod tidy` |
