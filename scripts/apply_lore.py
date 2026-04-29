"""Apply migration 080_seed_lore.sql to PostgreSQL."""
import os
import sys
import psycopg

pw = os.environ.get("RASA_DB_PASSWORD", "")
if not pw:
    print("ERROR: RASA_DB_PASSWORD not set")
    sys.exit(1)

with open("migrations/080_seed_lore.sql", encoding="utf-8") as f:
    raw = f.read()

# Remove \c commands (we connect directly to rasa_memory)
lines = []
for line in raw.split("\n"):
    stripped = line.strip()
    if stripped.startswith("\\c "):
        continue
    lines.append(line)

sql_content = "\n".join(lines)

# Connect directly to rasa_memory
conn = psycopg.connect(
    host="localhost", port=5432, user="postgres",
    password=pw, dbname="rasa_memory"
)
conn.autocommit = True

try:
    conn.execute(sql_content)
    print("Lore migration applied successfully.")
except Exception as e:
    print(f"Error (may be expected for re-runs): {e}")

# Verify
cur = conn.execute(
    "SELECT node_type, COUNT(*) FROM canonical_nodes GROUP BY node_type ORDER BY node_type"
)
print("\nCanonical nodes by type:")
for row in cur.fetchall():
    print(f"  {row[0]:<20} {row[1]}")

cur2 = conn.execute("SELECT soul_id, agent_role FROM soul_sheets")
print("\nSoul sheets:")
for row in cur2.fetchall():
    print(f"  {row[0]:<20} {row[1]}")

conn.close()
print("\nDone.")
