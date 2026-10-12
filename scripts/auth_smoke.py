#!/usr/bin/env python3
"""Verify real CLI/API authentication without logging or retaining credentials."""
from contextlib import contextmanager
import json
import os
from pathlib import Path
import pty
import secrets
import select
import signal
import socket
import sqlite3
import subprocess
import tempfile
import termios
import time
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parents[1]
BINARY = ROOT / "bin" / "family-location"
HTTP = urllib.request.build_opener(urllib.request.ProxyHandler({}))


def check_password_interrupt(db):
    # Keep the PTY open after exit so the check measures actual restoration,
    # rather than relying on a parent shell to reset terminal flags.
    for interrupt in (signal.SIGINT, signal.SIGTERM):
        master, slave = pty.openpty()
        process = None
        try:
            original = termios.tcgetattr(slave)
            process = subprocess.Popen(
                [str(BINARY), "init-admin", "--username", "interrupt-test", "--family", "测试家庭"],
                cwd=ROOT, env=dict(os.environ, FAMILY_DB_PATH=str(db)),
                stdin=slave, stdout=slave, stderr=slave,
            )
            deadline = time.monotonic() + 5
            while termios.tcgetattr(slave)[3] & termios.ECHO:
                if process.poll() is not None or time.monotonic() >= deadline:
                    raise AssertionError("password prompt did not disable echo")
                readable, _, _ = select.select([master], [], [], 0.02)
                if readable:
                    os.read(master, 4096)
            process.send_signal(interrupt)
            assert process.wait(timeout=5) != 0
            assert termios.tcgetattr(slave) == original, "terminal flags not restored after password interruption"
            assert not db.exists(), "interrupted initialization created a database"
        finally:
            if process is not None and process.poll() is None:
                process.kill()
                process.wait(timeout=5)
            os.close(master)
            os.close(slave)


def request(base, method, path, expected, body=None, cookie=None, csrf=None, token=None):
    headers = {"Origin": base}
    data = None
    if body is not None:
        data = json.dumps(body).encode()
        headers["Content-Type"] = "application/json"
    if cookie:
        headers["Cookie"] = cookie
    if csrf:
        headers["X-CSRF-Token"] = csrf
    if token:
        headers["Authorization"] = "Bearer " + token
    req = urllib.request.Request(base + path, data=data, headers=headers, method=method)
    try:
        response = HTTP.open(req, timeout=5)
    except urllib.error.HTTPError as error:
        response = error
    with response:
        assert response.status == expected, f"{method} {path}: {response.status}, expected {expected}"
        assert response.headers["Cache-Control"] == "no-store"
        data = response.read()
        return json.loads(data) if data else None, response.headers


@contextmanager
def server(db, log_path):
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        port = sock.getsockname()[1]
    base = f"http://127.0.0.1:{port}"
    env = dict(os.environ, FAMILY_DB_PATH=str(db), FAMILY_HTTP_ADDR=f"127.0.0.1:{port}")
    with log_path.open("wb") as log:
        process = subprocess.Popen([str(BINARY)], cwd=ROOT, env=env, stdout=log, stderr=log)
        try:
            deadline = time.monotonic() + 15
            while True:
                if process.poll() is not None:
                    raise AssertionError("server exited during startup")
                try:
                    request(base, "GET", "/readyz", 200)
                    break
                except (urllib.error.URLError, TimeoutError):
                    if time.monotonic() > deadline:
                        raise AssertionError("server readiness timeout")
                    time.sleep(0.05)
            yield base
            process.send_signal(signal.SIGTERM)
            assert process.wait(timeout=12) == 0
        finally:
            if process.poll() is None:
                process.kill()
                process.wait(timeout=5)


def main():
    if not BINARY.exists():
        raise SystemExit("Run make build first")
    tmp_root = ROOT / ".cache" / "tmp"
    tmp_root.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="auth-smoke-", dir=tmp_root) as tmp:
        tmp = Path(tmp)
        check_password_interrupt(tmp / "interrupted.db")
        db = tmp / "family.db"
        # Start from the exact v1 shape and check the CLI upgrades without data loss.
        with sqlite3.connect(db) as connection:
            connection.executescript("""
                CREATE TABLE schema_migrations (
                    version INTEGER PRIMARY KEY CHECK (version > 0),
                    applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
                );
                INSERT INTO schema_migrations(version) VALUES (1);
                CREATE TABLE retained(value TEXT);
                INSERT INTO retained VALUES ('kept');
            """)
        password = secrets.token_urlsafe(24)
        result = subprocess.run(
            [str(BINARY), "init-admin", "--username", "smoke-admin", "--family", "测试家庭", "--password-stdin"],
            cwd=ROOT, env=dict(os.environ, FAMILY_DB_PATH=str(db), FAMILY_HTTP_ADDR=":8080"),
            input=(password + "\n").encode(), stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=15,
        )
        assert result.returncode == 0, "admin initialization failed"
        assert password.encode() not in result.stdout
        with sqlite3.connect(db) as connection:
            assert connection.execute("SELECT value FROM retained").fetchone() == ("kept",)
            assert connection.execute("SELECT version FROM schema_migrations ORDER BY version").fetchall() == [(1,), (2,)]
        with server(db, tmp / "first.log") as base:
            request(base, "GET", "/api/v1/auth/me", 401)
            session, headers = request(base, "POST", "/api/v1/auth/login", 200,
                                       {"username": "smoke-admin", "password": password})
            cookie = headers["Set-Cookie"].split(";", 1)[0]
            csrf = session["csrf_token"]
            request(base, "POST", "/api/v1/pairing-codes", 403, {}, cookie=cookie)
            pairing, _ = request(base, "POST", "/api/v1/pairing-codes", 201, {}, cookie=cookie, csrf=csrf)
            payload = {"pairing_id": pairing["pairing_id"], "code": pairing["code"], "name": "测试设备"}
            paired, _ = request(base, "POST", "/api/v1/devices/pair", 201, payload)
            token = paired["device_token"]
            device_id = paired["device"]["id"]
            request(base, "POST", "/api/v1/devices/pair", 400, payload)
            request(base, "GET", "/api/v1/device/me", 200, token=token)
            request(base, "GET", "/api/v1/devices", 401, token=token)
            devices, _ = request(base, "GET", "/api/v1/devices", 200, cookie=cookie)
            assert len(devices["devices"]) == 1
            assert token not in json.dumps(devices)
        with server(db, tmp / "second.log") as base:
            request(base, "GET", "/api/v1/auth/me", 200, cookie=cookie)
            request(base, "GET", "/api/v1/device/me", 200, token=token)
            request(base, "DELETE", "/api/v1/devices/" + device_id, 204, cookie=cookie, csrf=csrf)
            request(base, "GET", "/api/v1/device/me", 401, token=token)
            request(base, "POST", "/api/v1/auth/logout", 204, cookie=cookie, csrf=csrf)
            request(base, "GET", "/api/v1/auth/me", 401, cookie=cookie)
        for log_path in (tmp / "first.log", tmp / "second.log"):
            text = log_path.read_text()
            for secret in (password, token, cookie.split("=", 1)[1], pairing["code"]):
                assert secret not in text, "credential found in server log"
    print("PASS: v1 migration, admin CLI, login/CSRF, pairing, device permissions, restart, revoke/logout")


if __name__ == "__main__":
    main()
