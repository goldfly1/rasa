# Developer Setup Guide

## Prerequisites

| Component | Version | Install Command/Location |
|-----------|---------|------------------------|
| Windows 11 | 23H2+ | System Settings |
| WSL2 + Ubuntu | 22.04+ | `wsl --install -d Ubuntu` |
| Python (Windows) | 3.12.x | `tests/smoke_test_deps.ps1` installs from MS Store |
| Python (WSL) | 3.12.x | `sudo apt install python3.12 python3.12-venv` |
| PostgreSQL | 15+ | `scoop install postgresql` or EDB installer |
| Redis | 7.x | `scoop install redis` |
| Ollama | latest | `ollama.com/download/windows` |
| Git | 2.40+ | `scoop install git` |

## One-Time Environment Bootstrap

```powershell
# From PowerShell (Admin)
cd ~\rasa
.\tests\setup_windows.ps1       # Installs MS Store Python, pip, deps
.\tests\smoke_test_deps.ps1   # Verifies everything, installs dev packages

# From WSL
cd /mnt/c/Users/goldf/rasa
python3.12 -m venv .venv
cp soul_template.py .hermes/SOUL.md   # Or copy your actual SOUL.md
```

## Daily Development Workflow

### 1. Start Services (Windows side)
```powershell
# Terminal 1: PostgreSQL
pg_ctl start -D $env:PGDATA

# Terminal 2: Redis
redis-server

# Terminal 3: Ollama (if not running as service)
ollama serve

# Verify all are listening
Get-NetTCPConnection -LocalPort 5432,6379,11434 | Select LocalPort,State
```

### 2. Initialize Database (one-time, or after schema changes)
```powershell
cd ~\rasa
psql -U postgres -d postgres -f schema.sql
```

### 3. Run Component Tests
```powershell
# Test dispatcher → DB flow
.\tests\smoke_dispatcher.ps1 "What files are in Downloads?"

# Test full round-trip (creates PowerShell worker)
# (Not yet implemented — Phase 3)
```

### 4. Develop a New Tool/Worker

Add to `AGENTS.md`:
```markdown
### my_new_tool
- model: `llama3.2`
- endpoint: `/api/tags`
- tool: `[{"type": "function", "function": {"name": "run_my_tool"}}]`
- role: "You are a file organizer"
- input_format: JSON with `source_dir`, `target_dir`
- output_format: JSON array of `moved_files`
- system: "System prompt here"
```

Create worker:
```python
# tests/run_my_tool.py
import sys, json, shutil, os

def main():
    task = json.load(sys.stdin)
    src = task['source_dir']
    dst = task['target_dir']
    moved = []
    for f in os.listdir(src):
        shutil.move(os.path.join(src, f), dst)
        moved.append(f)
    print(json.dumps({'status': 'ok', 'moved_files': moved}))

if __name__ == '__main__':
    main()
```

## Debugging

### Watch the task queue live
```sql
-- In psql
\x auto
SELECT id, agent_name, status, prompt, created_at FROM rasa_orch.tasks ORDER BY id DESC LIMIT 5;
```

### Check Ollama directly from WSL
```bash
curl http://localhost:11434/api/generate -d '{
  "model": "llama3.2",
  "prompt": "Say hello",
  "stream": false
}' | python3 -m json.tool
```

### Trace PowerShell → DB
```powershell
# Add to top of llm_gateway.ps1
$VerbosePreference = 'Continue'
# Then run: .\scripts\llm_gateway.ps1 -structuredOutput '{"tool":"hello"}'
```

## Project Layout Quick Reference

```
rasa/
├── .hermes/
│   └── SOUL.md              ← Your project config (edit to match your system)
├── agents/
│   └── AGENTS.md            ← Agent registry (add new agents here)
├── docs/                    ← All architecture + dev docs
├── scripts/
│   ├── agent_dispatcher.py  ← WSL-side dispatcher
│   └── llm_gateway.ps1      ← Windows-side DB gateway
├── tests/
│   ├── setup_windows.ps1    ← Bootstrap script
│   ├── smoke_test_deps.ps1  ← Dependency smoke tests
│   ├── smoke_dispatcher.ps1 ← Dispatcher integration test
│   └── run_test_agent.py    ← Example worker
├── schema.sql               ← DB schema
└── README.md
```

## Common Issues

| Symptom | Cause | Fix |
|---------|-------|-----|
| `ModuleNotFoundError: No module named 'jinja2'` | Python packages not installed | Run `smoke_test_deps.ps1` |
| `Connection refused` to 5432 | PostgreSQL not running | `pg_ctl start` |
| `psql: FATAL password authentication` | PGUSER not set | `$env:PGUSER='postgres'` |
| Dispatcher returns empty JSON | Ollama not responding | `ollama serve` |
| PowerShell hangs | WSL interop glitch | Restart WSL: `wsl --shutdown` |
