#!/usr/bin/env python3
"""Run the actual Kotlin DeviceApi against a disposable Go/SQLite server."""
import os
from pathlib import Path
import sqlite3
import subprocess
import tempfile

from auth_smoke import ROOT, request, server
from location_smoke import initialize, login, check_logs


def main():
    with tempfile.TemporaryDirectory(prefix="android-smoke-", dir=ROOT / ".cache/tmp") as directory:
        tmp = Path(directory)
        db = tmp / "android.db"
        sensitive = []
        password = initialize(db, "android-smoke-admin", sensitive)
        with server(db, tmp / "server.log") as base:
            admin = login(base, "android-smoke-admin", password, sensitive)
            pairing, _ = request(base, "POST", "/api/v1/pairing-codes", 201, {}, **admin)
            sensitive.extend((pairing["pairing_id"], pairing["code"]))
            env = dict(os.environ, ANDROID_SMOKE_ORIGIN=base,
                       ANDROID_SMOKE_PAIRING_ID=pairing["pairing_id"],
                       ANDROID_SMOKE_PAIRING_CODE=pairing["code"])
            result = subprocess.run(["sh", str(ROOT / "scripts/android.sh"), ":core:test", "--tests",
                                     "com.jk.family.core.DeviceApiSmokeTest", "--rerun-tasks"],
                                    cwd=ROOT, env=env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                                    timeout=240)
            # Gradle/test output must not contain temporary pairing data.
            output = result.stdout.decode(errors="replace")
            for value in sensitive:
                assert value not in output, "sensitive data in Kotlin test output"
            if result.returncode:
                print(output)
                raise RuntimeError("Android DeviceApi smoke failed")
            locations, _ = request(base, "GET", "/api/v1/locations", 200, **admin)
            assert locations["locations"] == [], "stop did not clear remote location"
            with sqlite3.connect(db) as connection:
                assert connection.execute("SELECT COUNT(*) FROM devices").fetchone() == (1,)
                assert connection.execute("SELECT COUNT(*) FROM latest_locations").fetchone() == (0,)
        check_logs(tmp, sensitive)
    print("Android Kotlin DeviceApi + isolated Go backend smoke passed; no real device/location used.")


if __name__ == "__main__":
    main()
