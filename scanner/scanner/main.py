# scanner/scanner/main.py
import logging
import queue
import signal
import sys
import threading

from scanner import db
from scanner.api import server as api_server
from scanner.config import load as load_config
from scanner.loops import download_worker, move_worker, scan_loop

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s %(levelname)s %(name)s %(message)s",
)
logger = logging.getLogger(__name__)


def main() -> None:
    cfg = load_config()
    db.init(cfg.database_url)
    mq: queue.Queue = queue.Queue()

    threads = [
        threading.Thread(target=scan_loop.run, args=(cfg,), name="scan_loop", daemon=True),
        threading.Thread(target=api_server.run, args=(cfg, mq), name="api_server", daemon=True),
        threading.Thread(target=move_worker.run, args=(cfg, mq), name="move_worker", daemon=True),
        threading.Thread(target=download_worker.run, args=(cfg,), name="download_worker", daemon=True),
    ]

    for t in threads:
        t.start()
        logger.info("started thread %s", t.name)

    stop = threading.Event()

    def _handle_signal(signum, frame):  # noqa: ANN001
        logger.info("received signal %d, shutting down", signum)
        stop.set()

    signal.signal(signal.SIGINT, _handle_signal)
    signal.signal(signal.SIGTERM, _handle_signal)

    stop.wait()
    logger.info("scanner stopped")
    sys.exit(0)


if __name__ == "__main__":
    main()
