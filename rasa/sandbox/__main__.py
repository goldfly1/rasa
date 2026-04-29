"""Sandbox daemon entry point.

Usage: python -m rasa.sandbox --data-dir data/sandbox
"""

from __future__ import annotations

import argparse
import asyncio
import signal

from rasa.sandbox.pipeline import SandboxPipeline


async def _main() -> None:
    parser = argparse.ArgumentParser(description="RASA Sandbox Pipeline")
    parser.add_argument("--data-dir", default="data/sandbox", help="Sandbox root directory")
    parser.add_argument("--working-dir", default=None, help="Working directory to promote into")
    parser.add_argument("--dbname", default="rasa_orch", help="PostgreSQL database name")
    args = parser.parse_args()

    pipeline = SandboxPipeline(data_dir=args.data_dir, working_dir=args.working_dir)
    print(f"Sandbox pipeline starting (data={args.data_dir})")

    stop_event = asyncio.Event()

    def _on_signal():
        print("\n[sandbox] shutting down...")
        stop_event.set()

    loop = asyncio.get_running_loop()
    try:
        loop.add_signal_handler(signal.SIGINT, _on_signal)
        loop.add_signal_handler(signal.SIGTERM, _on_signal)
    except NotImplementedError:
        pass  # Windows

    task = asyncio.create_task(pipeline.start(dbname=args.dbname))
    await stop_event.wait()
    task.cancel()
    try:
        await task
    except asyncio.CancelledError:
        pass
    await pipeline.stop()
    print("[sandbox] stopped")


def main() -> None:
    try:
        asyncio.run(_main())
    except KeyboardInterrupt:
        print("[sandbox] interrupted")


if __name__ == "__main__":
    main()
