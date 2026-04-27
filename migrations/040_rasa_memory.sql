-- Database: rasa_memory
-- Memory Subsystem — canonical model, embeddings, soul sheets

\c rasa_memory;

CREATE EXTENSION IF NOT EXISTS "pgcrypto";
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "vector";

-- Canonical model (JSONB + indexed FKs) ----------------------------------
CREATE TABLE canonical_nodes (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    node_type   TEXT NOT NULL,
    name        TEXT NOT NULL,
    path        TEXT,
    body        JSONB NOT NULL DEFAULT '{}',
    outgoing_edges UUID[] NOT NULL DEFAULT '{}',
    created_at  TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_cn_type ON canonical_nodes(node_type);
CREATE INDEX idx_cn_name ON canonical_nodes(name);
CREATE INDEX idx_cn_body ON canonical_nodes USING GIN(body);

-- Embeddings ---------------------------------------------------------------
CREATE TABLE embeddings (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    node_id     UUID REFERENCES canonical_nodes(id) ON DELETE CASCADE,
    model       TEXT NOT NULL,
    chunk_index INTEGER NOT NULL DEFAULT 0,
    chunk_text  TEXT NOT NULL,
    embedding   vector(768) NOT NULL,
    created_at  TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_emb_node ON embeddings(node_id);
CREATE INDEX idx_emb_vector ON embeddings USING hnsw (embedding vector_cosine_ops)
WITH (m = 16, ef_construction = 64);

-- Soul sheets (runtime cache) --------------------------------------------
CREATE TABLE soul_sheets (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    soul_id     TEXT NOT NULL UNIQUE,
    version     TEXT NOT NULL,
    agent_role  TEXT NOT NULL,
    body        JSONB NOT NULL,
    source_path TEXT,
    updated_at  TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    created_at  TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_soul_role ON soul_sheets(agent_role);
