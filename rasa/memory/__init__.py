"""Memory subsystem — embeddings, canonical model, eviction."""
from rasa.memory.embedder import embed_loop, main
from rasa.memory.pgvector import semantic_search, upsert_embedding

__all__ = ["embed_loop", "main", "semantic_search", "upsert_embedding"]
