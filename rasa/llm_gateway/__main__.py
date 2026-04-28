"""Standalone LLM Gateway entry point.

Usage: python -m rasa.llm_gateway --config config/gateway.yaml
"""

from __future__ import annotations

import argparse
import asyncio

from rasa.llm_gateway.client import GatewayClient


async def _main() -> None:
    parser = argparse.ArgumentParser(description="RASA LLM Gateway")
    parser.add_argument("--config", default="config/gateway.yaml")
    args = parser.parse_args()

    client = GatewayClient(config_path=args.config)
    print(f"LLM Gateway ready (config={args.config})")
    print("Tiers:", ", ".join(client._router.tiers))
    print("Ctrl-C to stop")

    try:
        await asyncio.get_running_loop().create_future()
    except asyncio.CancelledError:
        pass
    finally:
        await client.close()


def main() -> None:
    try:
        asyncio.run(_main())
    except KeyboardInterrupt:
        print("LLM Gateway stopped")


if __name__ == "__main__":
    main()
