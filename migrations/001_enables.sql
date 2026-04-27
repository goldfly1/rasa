-- Enable extensions on all RASA databases
-- Run this once per database, or use bootstrap_schema.ps1

CREATE EXTENSION IF NOT EXISTS "pgcrypto";
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
