#!/usr/bin/env python3
"""Exercise location APIs or serve clearly labelled, isolated simulated devices."""
import argparse
from datetime import datetime, timedelta, timezone
import os
from pathlib import Path
import secrets
import signal
import sqlite3
import subprocess
import sys
import tempfile
import time

sys.dont_write_bytecode = True
from auth_smoke import BINARY, ROOT, request, server


def initialize(db, username, sensitive):
    password = secrets.token_urlsafe(24)
    sensitive.append(password)
    result = subprocess.run(
        [str(BINARY), "init-admin", "--username", username,
         "--family", "模拟测试家庭-" + username, "--password-stdin"],
        cwd=ROOT, env=dict(os.environ, FAMILY_DB_PATH=str(db), FAMILY_HTTP_ADDR="127.0.0.1:8080"),
        input=(password + "\n").encode(), stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=15,
    )
    assert result.returncode == 0, "temporary admin initialization failed"
    assert password.encode() not in result.stdout, "password found in initialization output"
    return password


def login(base, username, password, sensitive):
    session, headers = request(base, "POST", "/api/v1/auth/login", 200,
                               {"username": username, "password": password})
    cookie = headers["Set-Cookie"].split(";", 1)[0]
    sensitive.extend((cookie.split("=", 1)[1], session["csrf_token"]))
    return {"cookie": cookie, "csrf": session["csrf_token"]}


def pair(base, admin, name, sensitive):
    pairing, _ = request(base, "POST", "/api/v1/pairing-codes", 201, {}, **admin)
    paired, _ = request(base, "POST", "/api/v1/devices/pair", 201,
                        {"pairing_id": pairing["pairing_id"], "code": pairing["code"], "name": name})
    sensitive.extend((pairing["code"], paired["device_token"]))
    return paired["device"]["id"], paired["device_token"]


def start(base, token, sensitive):
    result, _ = request(base, "POST", "/api/v1/device/sharing/start", 201, {}, token=token)
    sensitive.append(result["sharing_session_id"])
    return result["sharing_session_id"]


def point(session, sequence, age_minutes=0, offset=0):
    # Fictional points near a public park, never copied from a person's location.
    captured = datetime.now(timezone.utc) - timedelta(minutes=age_minutes)
    return {"sharing_session_id": session, "sequence": sequence,
            "latitude": round(39.990321 + offset, 6), "longitude": round(116.390789 + offset, 6),
            "coordinate_system": "GCJ-02", "accuracy_m": 18.5, "battery_percent": None,
            "captured_at": captured.isoformat(timespec="milliseconds").replace("+00:00", "Z")}


def upload(base, token, payload, accepted=True, status=200):
    result, _ = request(base, "PUT", "/api/v1/device/location", status, payload, token=token)
    if status == 200:
        assert result["accepted"] == accepted, "unexpected upload acceptance"
        assert result["config"] == {"version": 1, "moving_interval_seconds": 120, "stationary_interval_seconds": 300, "moving_high_accuracy": False, "quiet_hours_start": "22:00", "quiet_hours_end": "08:00"}, "default config missing"


def locations(base, admin):
    result, _ = request(base, "GET", "/api/v1/locations", 200, **admin)
    assert result["server_time"], "missing server time"
    return result["locations"]


def check_logs(tmp, sensitive):
    for path in tmp.glob("*.log"):
        text = path.read_text()
        for value in sensitive:
            assert str(value) not in text, "sensitive value found in server log"


def smoke(db, tmp, password, sensitive):
    other_password = initialize(db, "other-admin", sensitive)
    with server(db, tmp / "first.log") as base:
        admin = login(base, "location-admin", password, sensitive)
        other = login(base, "other-admin", other_password, sensitive)
        device_id, token = pair(base, admin, "模拟设备-冒烟", sensitive)
        assert locations(base, admin) == [], "pairing unexpectedly enabled sharing"
        upload(base, token, point("not-started", 1), status=409)
        request(base, "GET", "/api/v1/locations", 401, token=token)
        session = start(base, token, sensitive)
        assert locations(base, admin) == [], "start unexpectedly invented a location"
        first = point(session, 1, age_minutes=9)
        upload(base, token, first)
        latest = point(session, 2, age_minutes=8, offset=0.001)
        latest["battery_percent"] = 62
        sensitive.extend((first["latitude"], first["longitude"], latest["latitude"], latest["longitude"]))
        upload(base, token, latest)
        snapshot = locations(base, admin)
        assert len(snapshot) == 1 and snapshot[0]["device_id"] == device_id
        saved = snapshot[0]
        assert saved["sequence"] == 2 and saved["stale"] is True
        for key in ("latitude", "longitude", "accuracy_m", "battery_percent", "coordinate_system"):
            assert saved[key] == latest[key], "latest location field mismatch: " + key
        assert datetime.fromisoformat(saved["captured_at"].replace("Z", "+00:00")) == datetime.fromisoformat(latest["captured_at"].replace("Z", "+00:00"))
        assert saved["captured_at"] != saved["received_at"], "capture time replaced by receive time"
        with sqlite3.connect(db) as connection:
            assert connection.execute("SELECT COUNT(*) FROM latest_locations").fetchone() == (1,)
        time.sleep(0.03)
        upload(base, token, latest, accepted=False)
        assert locations(base, admin) == snapshot, "duplicate upload changed location or renewed lease"
        upload(base, token, first, status=409)
        backwards = dict(first, sequence=3)
        upload(base, token, backwards, status=409)
        assert locations(base, other) == [], "cross-family location disclosure"
        request(base, "DELETE", "/api/v1/devices/" + device_id, 404, **other)

    with server(db, tmp / "restart.log") as base:
        assert locations(base, admin) == snapshot, "restart changed persisted location"
        upload(base, token, latest, accepted=False)
        upload(base, token, first, status=409)
        request(base, "POST", "/api/v1/device/sharing/stop", 204,
                {"sharing_session_id": session}, token=token)
        assert locations(base, admin) == [], "stop did not hide location"
        upload(base, token, dict(latest, sequence=3), status=409)
        new_session = start(base, token, sensitive)
        assert new_session != session
        upload(base, token, dict(latest, sequence=3), status=409)
        request(base, "POST", "/api/v1/device/sharing/stop", 409,
                {"sharing_session_id": session}, token=token)
        current = point(new_session, 1)
        upload(base, token, current)
        assert len(locations(base, admin)) == 1
        request(base, "DELETE", "/api/v1/devices/" + device_id, 204, **admin)
        assert locations(base, admin) == [], "revoke did not hide location"
        upload(base, token, dict(current, sequence=2), status=401)
        _, expiring_token = pair(base, admin, "模拟设备-租约过期", sensitive)
        expiring_session = start(base, expiring_token, sensitive)
        expired = point(expiring_session, 1)
        upload(base, expiring_token, expired)

    # Manipulate only our stopped, disposable database to avoid waiting seven minutes.
    with sqlite3.connect(db) as connection:
        connection.execute("UPDATE latest_locations SET received_at=?, lease_expires_at=?", (int(time.time() * 1000) - 421000, int(time.time() * 1000) - 1000))
    with server(db, tmp / "cleanup.log") as base:
        retained = locations(base, admin)
        assert len(retained) == 1 and retained[0]["stale"], "last location must survive timeout and restart"
        with sqlite3.connect(db) as connection:
            assert connection.execute("SELECT COUNT(*) FROM latest_locations").fetchone()[0] == 1
        upload(base, expiring_token, expired, accepted=False)
        assert locations(base, admin) == retained, "retry changed last position or renewed freshness"
        upload(base, expiring_token, point(expiring_session, 2))
        assert len(locations(base, admin)) == 1
    check_logs(tmp, sensitive)
    print("PASS: 隔离库初始化/配对、默认不共享、唯一最新位置、乱序/重复、原始时间/stale、跨家庭隔离、重启、停止/旧会话、撤销、超时及重启保留、日志脱敏")


def demo(db, tmp, password, sensitive):
    def interrupt(signum, frame):
        raise KeyboardInterrupt

    previous = signal.signal(signal.SIGTERM, interrupt)
    try:
        with server(db, tmp / "demo.log") as base:
            try:
                admin = login(base, "location-admin", password, sensitive)
                devices = []
                for name in ("模拟设备-A（非真实手机）", "模拟设备-B（非真实手机）"):
                    _, token = pair(base, admin, name, sensitive)
                    devices.append((token, start(base, token, sensitive)))
                print("隔离模拟服务：" + base, flush=True)
                print("网页与位置 API 已就绪；位置 API：" + base + "/api/v1/locations（需管理员登录 Cookie）", flush=True)
                print("临时管理员用户名：location-admin", flush=True)
                print("临时随机密码（仅当前隔离模拟库，退出即销毁）：" + password, flush=True)
                print("两台设备及坐标均为模拟数据；每 30 秒更新，Ctrl+C 停止并清理。", flush=True)
                sequence = 1
                while True:
                    for index, (token, session) in enumerate(devices):
                        payload = point(session, sequence, offset=index * 0.002 + (sequence % 20) * 0.00001)
                        payload["battery_percent"] = 80 - index * 10
                        upload(base, token, payload)
                        check_logs(tmp, (payload["latitude"], payload["longitude"]))
                    print(f"模拟位置已更新：2 台设备，序号 {sequence}", flush=True)
                    sequence += 1
                    time.sleep(30)
            except KeyboardInterrupt:
                print("正在停止隔离模拟服务并清理临时数据库。", flush=True)
        check_logs(tmp, sensitive)
    finally:
        signal.signal(signal.SIGTERM, previous)


def main():
    parser = argparse.ArgumentParser(description="位置 API 冒烟测试；仅使用项目内独立临时数据库。")
    parser.add_argument("--demo", action="store_true", help="启动两台模拟设备，每 30 秒上传；Ctrl+C 清理退出")
    args = parser.parse_args()
    if args.demo and not sys.stdout.isatty():
        parser.error("--demo 必须在终端运行，临时密码不允许重定向到文件")
    if not BINARY.exists():
        raise SystemExit("请先运行 make build")
    tmp_root = ROOT / ".cache" / "tmp"
    tmp_root.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="location-demo-" if args.demo else "location-smoke-", dir=tmp_root) as directory:
        tmp = Path(directory)
        db = tmp / "family.db"
        sensitive = []
        password = initialize(db, "location-admin", sensitive)
        if args.demo:
            demo(db, tmp, password, sensitive)
        else:
            smoke(db, tmp, password, sensitive)


if __name__ == "__main__":
    main()
