from rasa.bus import Envelope, Metadata, PostgresPublisher, PostgresSubscriber, RedisPublisher, RedisSubscriber
e = Envelope.new("test", "dest", {"hello": "world"}, Metadata(soul_id="coder-v2-dev"))
print("OK:", e.message_id)
print("JSON:", e.to_json())
e2 = Envelope.from_json(e.to_json())
print("Round-trip:", e2.message_id == e.message_id)
print("All imports OK")
