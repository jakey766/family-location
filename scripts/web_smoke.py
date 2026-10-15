#!/usr/bin/env python3
"""DOM or optional browser checks against disposable project-local data."""
import os
from pathlib import Path
import subprocess
import sys
import tempfile
sys.dont_write_bytecode = True
from location_smoke import initialize, login, pair, start, point, upload, check_logs
from auth_smoke import ROOT, server

def main():
    # This suite intentionally tests the no-map fallback, regardless of user config.
    os.environ["FAMILY_AMAP_JS_KEY"] = ""
    os.environ["FAMILY_AMAP_SECURITY_JSCODE"] = ""
    with tempfile.TemporaryDirectory(prefix='web-smoke-', dir=ROOT / '.cache' / 'tmp') as directory:
        tmp = Path(directory)
        db = tmp / 'family.db'
        sensitive = []
        password = initialize(db, 'web-admin', sensitive)
        with server(db, tmp / 'web.log') as base:
            admin = login(base, 'web-admin', password, sensitive)
            for index, name in enumerate(('模拟设备-A', '模拟设备-B')):
                _, token = pair(base, admin, name, sensitive)
                sharing = start(base, token, sensitive)
                payload = point(sharing, 1, age_minutes=index * 6, offset=index * .002)
                payload['battery_percent'] = 80 if index == 0 else None
                upload(base, token, payload)
                sensitive.extend((payload['latitude'], payload['longitude']))
            env = dict(os.environ, WEB_TEST_URL=base, WEB_TEST_USERNAME='web-admin', WEB_TEST_PASSWORD=password,
                       PLAYWRIGHT_BROWSERS_PATH=str(ROOT / '.cache' / 'ms-playwright'), TMPDIR=str(ROOT / '.cache' / 'tmp'))
            script = 'web_browser_test.mjs' if '--browser' in sys.argv else 'web_dom_test.mjs'
            command = ['node'] + ([] if '--browser' in sys.argv else ['--experimental-vm-modules']) + [str(ROOT / 'scripts' / script)]
            result = subprocess.run(command, cwd=ROOT, env=env, timeout=180)
            if result.returncode:
                raise SystemExit(result.returncode)
        check_logs(tmp, sensitive)
    print('PASS: isolated real-backend web checks; processes and database cleaned')
if __name__ == '__main__':
    main()
