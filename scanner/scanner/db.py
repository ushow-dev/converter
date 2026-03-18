# scanner/scanner/db.py
import logging
from pathlib import Path

import psycopg2
import psycopg2.pool

logger = logging.getLogger(__name__)

_pool: psycopg2.pool.ThreadedConnectionPool | None = None


def init(database_url: str, minconn: int = 1, maxconn: int = 5) -> None:
    """Initialise the connection pool and run migrations."""
    global _pool
    _pool = psycopg2.pool.ThreadedConnectionPool(minconn, maxconn, dsn=database_url)
    _run_migrations()


def get_conn() -> psycopg2.extensions.connection:
    """Borrow a connection from the pool. Caller must call put_conn() afterwards."""
    if _pool is None:
        raise RuntimeError("DB pool not initialised — call db.init() first")
    return _pool.getconn()


def put_conn(conn: psycopg2.extensions.connection) -> None:
    """Return a connection to the pool."""
    if _pool is not None:
        _pool.putconn(conn)


def _run_migrations() -> None:
    migrations_dir = Path(__file__).parent / "migrations"
    sql_files = sorted(migrations_dir.glob("*.sql"))

    conn = get_conn()
    try:
        with conn:
            with conn.cursor() as cur:
                cur.execute("""
                    CREATE TABLE IF NOT EXISTS schema_migrations (
                        version TEXT PRIMARY KEY,
                        applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
                    )
                """)
                for path in sql_files:
                    version = path.stem
                    cur.execute(
                        "SELECT 1 FROM schema_migrations WHERE version = %s",
                        (version,),
                    )
                    if cur.fetchone():
                        continue
                    logger.info("applying migration %s", version)
                    cur.execute(path.read_text())
                    cur.execute(
                        "INSERT INTO schema_migrations (version) VALUES (%s)",
                        (version,),
                    )
    finally:
        put_conn(conn)
