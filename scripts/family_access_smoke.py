#!/usr/bin/env python3
"""Exercise family sharing against an isolated real server; never print secrets."""
from datetime import datetime, timezone
from pathlib import Path
import re
import secrets
import sys
import tempfile
import urllib.request

sys.dont_write_bytecode = True
from auth_smoke import ROOT, request, server
from location_smoke import initialize, login, pair, start, point, upload, check_logs


def passcode():
    return "0" + str(secrets.randbelow(1000000)).zfill(6)


def assert_passcode(value, sensitive):
    assert isinstance(value, str) and re.fullmatch(r"[0-9]{7}", value), "invalid generated passcode"
    sensitive.append(value)


def family_path(key, suffix=""):
    return "/api/v1/family/" + key + suffix


def authorize(base, key, code, sensitive, cookie=None):
    before = datetime.now(timezone.utc)
    result, headers = request(base, "POST", family_path(key, "/session"), 200,
                              {"passcode": code}, cookie=cookie)
    cookie = headers["Set-Cookie"].split(";", 1)[0]
    assert "HttpOnly" in headers["Set-Cookie"]
    assert "SameSite=" in headers["Set-Cookie"]
    expires = datetime.fromisoformat(result["expires_at"].replace("Z", "+00:00"))
    assert 7 * 86400 - 3 < (expires - before).total_seconds() <= 7 * 86400 + 1
    sensitive.append(cookie.split("=", 1)[1])
    return cookie, result["expires_at"]


def snapshot(base, key, cookie, ids, expiry=None):
    result, _ = request(base, "GET", family_path(key, "/snapshot"), 200, cookie=cookie)
    assert result["family"]["public_id"] == key
    assert {item["device_id"] for item in result["locations"]} == set(ids)
    assert all(set(d) <= {"id", "name", "revoked_at"} for d in result["devices"]), "viewer received management fields"
    if expiry is not None:
        assert result["expires_at"] == expiry, "reading silently extended authorization"
    return result


def html(base, path, marker):
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    with opener.open(base + path, timeout=5) as response:
        assert response.status == 200
        assert marker in response.read().decode(), "wrong page entry"


def main():
    tmp_root = ROOT / ".cache" / "tmp"
    tmp_root.mkdir(parents=True, exist_ok=True)
    sensitive = []
    with tempfile.TemporaryDirectory(prefix="family-access-smoke-", dir=tmp_root) as directory:
        tmp = Path(directory)
        db = tmp / "family.db"
        password = initialize(db, "family-owner", sensitive)
        other_password = initialize(db, "family-stranger", sensitive)
        with server(db, tmp / "first.log") as base:
            admin = login(base, "family-owner", password, sensitive)
            stranger = login(base, "family-stranger", other_password, sensitive)
            families, _ = request(base, "GET", "/api/v1/families", 200, **admin)
            first = families["families"][0]
            assert first["passcode_set"] is False, "CLI family must require initial passcode setup"
            key = first["public_id"]
            request(base, "GET", family_path(key, "/snapshot"), 401)
            code = passcode()
            sensitive.append(code)
            request(base, "PUT", "/api/v1/families/" + key + "/passcode", 200,
                    {"passcode": code}, **admin)
            second, _ = request(base, "POST", "/api/v1/families", 201, {"name": "模拟分享家庭"}, **admin)
            second_key = second["public_id"]
            assert_passcode(second["passcode"], sensitive)
            assert second["passcode_set"] is True
            device_id, token = pair(base, admin, "模拟跨家庭设备", sensitive)
            membership = "/api/v1/families/" + second_key + "/devices/" + device_id
            request(base, "PUT", membership, 204, {}, **admin)
            sharing = start(base, token, sensitive)
            upload(base, token, point(sharing, 1))
            all_positions, _ = request(base, "GET", "/api/v1/locations?scope=owned", 200, **admin)
            assert [p["device_id"] for p in all_positions["locations"]] == [device_id], "shared device was duplicated"
            request(base, "GET", "/api/v1/locations?family=" + key, 404, **stranger)
            request(base, "PUT", "/api/v1/families/" + key + "/passcode", 404, {}, **stranger)
            request(base, "DELETE", "/api/v1/families/" + key, 404, **stranger)
            request(base, "GET", family_path(key, "/snapshot"), 401, **admin)
            bad_code = "1" + code[1:]
            request(base, "POST", family_path(key, "/session"), 401, {"passcode": bad_code})
            viewer, first_expiry = authorize(base, key, code, sensitive)
            snapshot(base, key, viewer, [device_id], first_expiry)
            request(base, "GET", family_path(second_key, "/snapshot"), 401, cookie=viewer)
            viewer, second_expiry = authorize(base, second_key, second["passcode"], sensitive, viewer)
            snapshot(base, key, viewer, [device_id], first_expiry)
            snapshot(base, second_key, viewer, [device_id], second_expiry)
            for path in ("/api/v1/devices?scope=owned", "/api/v1/locations?scope=owned", "/api/v1/families", "/api/v1/devices/" + device_id + "/config"):
                request(base, "GET", path, 401, cookie=viewer)
            request(base, "DELETE", "/api/v1/devices/" + device_id, 401, cookie=viewer)
            request(base, "GET", family_path(key, "/web-config"), 200, cookie=viewer)
            request(base, "DELETE", "/api/v1/devices/" + device_id + "/purge", 409, **admin)
            request(base, "PATCH", "/api/v1/families/" + key, 200, {"name": "模拟新家庭名称"}, **admin)
            assert snapshot(base, key, viewer, [device_id])["family"]["name"] == "模拟新家庭名称"
            # Reset only one family's authorization; another grant must remain valid.
            reset, _ = request(base, "PUT", "/api/v1/families/" + key + "/passcode", 200, {}, **admin)
            assert_passcode(reset["passcode"], sensitive)
            request(base, "GET", family_path(key, "/snapshot"), 401, cookie=viewer)
            snapshot(base, second_key, viewer, [device_id], second_expiry)
            viewer, _ = authorize(base, key, reset["passcode"], sensitive, viewer)
            request(base, "DELETE", family_path(key, "/session"), 204, cookie=viewer)
            request(base, "GET", family_path(key, "/snapshot"), 401, cookie=viewer)
            snapshot(base, second_key, viewer, [device_id], second_expiry)
            for path, marker in (("/admin", "app.mjs"), ("/family?key=" + second_key, "family.mjs")):
                html(base, path, marker)

        with server(db, tmp / "restart.log") as base:
            snapshot(base, second_key, viewer, [device_id], second_expiry)
            request(base, "DELETE", "/api/v1/families/" + key, 204, **admin)
            snapshot(base, second_key, viewer, [device_id])
            request(base, "GET", family_path(key, "/snapshot"), 404, cookie=viewer)
            request(base, "DELETE", "/api/v1/families/" + second_key, 204, **admin)
            request(base, "GET", family_path(second_key, "/snapshot"), 404, cookie=viewer)
            families, _ = request(base, "GET", "/api/v1/families", 200, **admin)
            assert families["families"] == []
            owned, _ = request(base, "GET", "/api/v1/devices?scope=owned", 200, **admin)
            assert len(owned["devices"]) == 1 and owned["devices"][0]["families"] == []
            request(base, "GET", "/api/v1/devices/" + device_id + "/config", 200, **admin)
            upload(base, token, point(sharing, 2))
            all_positions, _ = request(base, "GET", "/api/v1/locations?scope=owned", 200, **admin)
            assert len(all_positions["locations"]) == 1, "family deletion removed device position or ownership"
            admin = login(base, "family-owner", password, sensitive)
            recreated, _ = request(base, "POST", "/api/v1/families", 201, {"name": "模拟重新创建家庭"}, **admin)
            assert_passcode(recreated["passcode"], sensitive)
            request(base, "DELETE", "/api/v1/devices/" + device_id, 204, **admin)
            request(base, "DELETE", "/api/v1/devices/" + device_id + "/purge", 204, **admin)
            owned, _ = request(base, "GET", "/api/v1/devices?scope=owned", 200, **admin)
            assert owned["devices"] == []
            request(base, "GET", "/api/v1/device/me", 401, token=token)
            request(base, "PUT", "/api/v1/device/location", 401, point(sharing, 3), token=token)
        check_logs(tmp, sensitive)
    print("PASS: family access, independent seven-day grants, restart, permission isolation, reset/logout, family deletion, unassigned positions, revoked-device purge")


if __name__ == "__main__":
    main()
