#!/usr/bin/env python3
"""Exercise the real server lifecycle; all artifacts stay inside the project."""
import json
import os
from pathlib import Path
import signal
import socket
import sqlite3
import subprocess
import tempfile
import time
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parents[1]
BINARY = ROOT / "bin" / "family-location"
# Local checks should not inherit a workstation's HTTP proxy.
HTTP = urllib.request.build_opener(urllib.request.ProxyHandler({}))


def check_endpoint(base, path, expected):
    with HTTP.open(base + path, timeout=2) as response:
        assert response.status == 200
        assert response.headers["Cache-Control"] == "no-store"
        assert json.load(response) == expected


def boot_and_stop(db, log_path):
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        port = sock.getsockname()[1]
    env = dict(os.environ, FAMILY_HTTP_ADDR=f"127.0.0.1:{port}", FAMILY_DB_PATH=str(db))
    base = f"http://127.0.0.1:{port}"
    with log_path.open("wb") as log:
        process = subprocess.Popen([str(BINARY)], cwd=ROOT, env=env, stdout=log, stderr=log)
        try:
            deadline = time.monotonic() + 15
            while True:
                if process.poll() is not None:
                    raise AssertionError(f"server exited early: {log_path.read_text()}")
                try:
                    check_endpoint(base, "/readyz", {"status": "ok", "database": "ok"})
                    break
                except (urllib.error.URLError, TimeoutError):
                    if time.monotonic() > deadline:
                        raise AssertionError("server did not become ready")
                    time.sleep(0.05)
            check_endpoint(base, "/healthz", {"status": "ok"})
            process.send_signal(signal.SIGTERM)
            assert process.wait(timeout=12) == 0, log_path.read_text()
        finally:
            if process.poll() is None:
                process.kill()
                process.wait(timeout=5)


def expect_startup_failure(address, db):
    result = subprocess.run(
        [str(BINARY)], cwd=ROOT,
        env=dict(os.environ, FAMILY_HTTP_ADDR=address, FAMILY_DB_PATH=str(db)),
        stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=15,
    )
    assert result.returncode != 0, "invalid startup unexpectedly succeeded"


def main():
    if not BINARY.exists():
        raise SystemExit("Run make build first; server binary is missing")
    (ROOT / ".cache" / "tmp").mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="smoke-", dir=ROOT / ".cache" / "tmp") as tmp:
        tmp = Path(tmp)
        db = tmp / "data" / "family.db"
        boot_and_stop(db, tmp / "first.log")
        with sqlite3.connect(db) as connection:
            assert connection.execute("SELECT version FROM schema_migrations ORDER BY version").fetchall() == [(1,), (2,)]
            connection.execute("CREATE TABLE smoke_persistence(value TEXT)")
            connection.execute("INSERT INTO smoke_persistence VALUES ('kept')")
        boot_and_stop(db, tmp / "second.log")
        with sqlite3.connect(db) as connection:
            assert connection.execute("SELECT value FROM smoke_persistence").fetchone() == ("kept",)
            assert connection.execute("SELECT COUNT(*) FROM schema_migrations").fetchone() == (2,)
        expect_startup_failure("127.0.0.1:8080", tmp)
        with socket.socket() as occupied:
            occupied.bind(("127.0.0.1", 0))
            occupied.listen()
            expect_startup_failure(f"127.0.0.1:{occupied.getsockname()[1]}", db)
    print("PASS: HTTP health/readiness, SQLite restart persistence, SIGTERM, startup failures")


if __name__ == "__main__":
    main()
