# Rasa Pilot — Procfile
# Start all services: honcho start
# Start a single service: honcho start <service>

# === Infrastructure ===
redis: redis-server --port 6379

# === Control Plane (Go) ===
# orchestrator is CLI-only (use: orchestrator submit --soul ... --title "..." --wait)
# orchestrator: orchestrator --db postgres://localhost/rasa_orch
pool-controller: pool-controller --config config/pool.yaml --redis localhost:6379 --http 127.0.0.1:8301 --db postgres://localhost/rasa_orch
policy-engine: policy-engine --db postgres://localhost/rasa_policy --redis localhost:6379 --soul-dir souls/
recovery: recovery-controller --db postgres://localhost/rasa_recovery
eval-aggregator: evaluation-engine --mode aggregator --db postgres://localhost/rasa_eval
memory: memory-controller --db postgres://localhost/rasa_memory --redis localhost:6379 --http 127.0.0.1:8300

# === Agent Layer (Python) ===
llm-gateway: python -m rasa.llm_gateway --config config/gateway.yaml
sandbox: python -m rasa.sandbox --db postgres://localhost/rasa_sandbox
eval-scorer: evaluation-engine --mode scorer --benchmarks benchmarks/

# === Agent Processes ===
agent-coder: python -m rasa.agent.runtime --soul souls/coder-v2-dev.yaml
agent-coder-2: python -m rasa.agent.runtime --soul souls/coder-v2-dev.yaml
agent-reviewer: python -m rasa.agent.runtime --soul souls/reviewer-v1.yaml
agent-planner: python -m rasa.agent.runtime --soul souls/planner-v1.yaml
agent-architect: python -m rasa.agent.runtime --soul souls/architect-v1.yaml
# === Observability ===
logs: python scripts/observe.py --watch logs/ --interval 60
