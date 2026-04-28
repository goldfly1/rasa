"""Debug Redis pub/sub delivery."""
import asyncio
import redis.asyncio as aioredis

async def main():
    received = []

    async def handler(data):
        received.append(data)

    # Raw test: subscribe, publish, check
    client = await aioredis.from_url("redis://localhost:6379")
    pubsub = client.pubsub()
    await pubsub.subscribe("debug_channel")

    # Drain in background
    async def drain():
        async for msg in pubsub.listen():
            print(f"DRAIN: type={msg.get('type')} channel={msg.get('channel')} data={msg.get('data')!r}")
            if msg["type"] == "message":
                data = msg["data"]
                if isinstance(data, bytes):
                    data = data.decode()
                received.append(data)

    task = asyncio.create_task(drain())
    await asyncio.sleep(0.2)

    # Publish
    print("PUBLISHING...")
    await client.publish("debug_channel", "hello world")
    print("PUBLISHED")

    await asyncio.sleep(0.5)
    task.cancel()

    print(f"Received: {received}")
    await client.aclose()

asyncio.run(main())
