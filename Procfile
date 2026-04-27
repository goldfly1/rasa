# Rasa Pilot — Procfile
# Start all services: honcho start
# Start a single service: honcho start <service>

# === Infrastructure ===
redis: redis-server --port 6379
nats: nats-server -c config/nats-server.conf

# === Control Plane (Go) ===
orchestrator: orchestrator --db postgres://localhost/rasa_orch --nats localhost:4222
pool-controller: pool-controller --config config/pool.yaml --db postgres://localhost/rasa_pool --nats localhost:4222
policy-engine: policy-engine --db postgres://localhost/rasa_policy --nats localhost:4222
recovery: recovery-controller --db postgres://localhost/rasa_recovery --nats localhost:4222
eval-aggregator: evaluation-engine --mode aggregator --db postgres://localhost/rasa_eval --nats localhost:4222
memory: memory-controller --db postgres://localhost/rasa_memory --nats localhost:4222

# === Agent Layer (Python) ===
llm-gateway: python -m rasa.llm_gateway --config config/gateway.yaml
sandbox: python -m rasa.sandbox --nats localhost:4222 --data-dir data/sandbox
eval-scorer: evaluation-engine --mode scorer --benchmarks benchmarks/

# === Agent Processes ===
agent-coder: python -m rasa.agent --soul souls/coder-v2-dev.yaml --mode daemon
agent-coder-2: python -m rasa.agent --soul souls/coder-v2-dev.yaml --mode daemon
agent-reviewer: python -m rasa.agent --soul souls/reviewer-v1.yaml --mode daemon
agent-planner: python -m rasa.agent --soul souls/planner-v1.yaml --mode daemon
agent-architect: python -m rasa.agent --soul souls/architect-v1.yaml --mode daemon

# === Observability ===
logs: python scripts/observe.py --watch logs/ --interval 60
