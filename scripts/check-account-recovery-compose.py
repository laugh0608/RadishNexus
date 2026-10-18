#!/usr/bin/env python3
"""ADR-0041 checks inside the existing isolated Compose test project.

Only synthetic fixture credentials are used. Inputs and outputs remain under the
parent script's private temporary directory and are removed by its cleanup trap.
"""
import http.cookiejar
import json
import os
from pathlib import Path
import ssl
import subprocess
import sys
import threading
import time
import urllib.error
import urllib.request
import urllib.parse
import uuid

run_dir = Path(sys.argv[1])
compose = json.loads(sys.argv[2])
origin = os.environ["RADISHNEXUS_PUBLIC_ORIGIN"]
context = ssl.create_default_context(cafile=str(run_dir / "caddy-root.crt"))
email = "admin@example.test"
old_password = "compose-browser-fixture-password-2026"
new_password = "compose-recovered-password-2026"
last_password = "compose-final-password-2026"
private_values = (email, old_password, new_password, last_password, "password_hash", "postgres://")
session_values = []


def command(action, request, expected=0):
    result = subprocess.run(
        compose + ["run", "--rm", "--no-deps", "-T", "account-recover", action, "--request-stdin"],
        input=json.dumps(request), text=True, capture_output=True, timeout=125, check=False,
    )
    if (result.returncode == 0) != (expected == 0):
        raise AssertionError(f"recovery {action}: unexpected exit classification")
    for value in private_values:
        assert value not in result.stdout + result.stderr, "CLI leaked private fixture data"
    return json.loads(result.stdout) if result.returncode == 0 else result.stderr


def api(opener, path, method="GET", body=None, csrf=None):
    headers = {"Origin": origin}
    data = None
    if body is not None:
        data = json.dumps(body).encode()
        headers["Content-Type"] = "application/json"
    if csrf:
        headers["X-CSRF-Token"] = csrf
    request = urllib.request.Request(origin + path, data=data, method=method, headers=headers)
    try:
        response = opener.open(request, timeout=25)
    except urllib.error.HTTPError as error:
        response = error
    with response:
        raw = response.read()
        return response.status, json.loads(raw) if raw else None


def browser_pause(stage):
    if os.environ.get("RADISHNEXUS_COMPOSE_BROWSER_REVIEW") != "true":
        return
    marker = run_dir / ("browser-" + stage + ".json")
    marker.write_text(json.dumps({"origin": origin, "stage": stage}))
    print("Browser review barrier: " + str(marker), flush=True)
    deadline = time.monotonic() + 600
    while not marker.with_suffix(".done").exists():
        if time.monotonic() > deadline:
            raise AssertionError("browser review deadline exceeded")
        time.sleep(0.2)


def login(password):
    jar = http.cookiejar.MozillaCookieJar()
    opener = urllib.request.build_opener(urllib.request.HTTPSHandler(context=context), urllib.request.HTTPCookieProcessor(jar))
    status, account = api(opener, "/api/v1/auth/sessions", "POST", {"email": email, "password": password})
    assert status == 201, "formal login failed"
    session_values.extend(c.value for c in jar)
    return jar, opener, account


def csrf(jar):
    return next(c.value for c in jar if c.name == "__Host-radishnexus-csrf")


def stream(opener, workspace):
    response = opener.open(origin + f"/api/v1/workspaces/{workspace}/channels/chn_compose/events", timeout=25)
    ready = []
    for _ in range(8):
        line = response.readline().decode()
        ready.append(line)
        if line == "\n":
            break
    assert "event: ready\n" in ready, "missing SSE ready frame"
    lines, errors = [], []
    def consume():
        try:
            for line in response:
                lines.append(line.decode())
        except Exception as error:
            errors.append(type(error).__name__)
        finally:
            response.close()
    thread = threading.Thread(target=consume, daemon=True)
    thread.start()
    return thread, lines, errors


jar, client, account = login(old_password)
user_id, workspace_id = account["user"]["id"], account["workspaces"][0]["id"]
# No guessed public recovery route exists, including for a Workspace owner.
assert api(client, "/api/v1/auth/recover", "POST", {})[0] == 404
browser_pause("before-recovery")
first_stream = stream(client, workspace_id)
inspection = command("inspect", {"email": email})
assert inspection["eligible"] and inspection["user_id"] == user_id
request = {
    "user_id": user_id, "expected_email": email,
    "expected_password_changed_at": inspection["password_changed_at"],
    "operation_id": str(uuid.uuid4()), "operator_ref": "ops-compose",
    "reason_code": "lost_local_password", "new_password": new_password, "confirmed": True,
}
result = command("apply", request)
assert result["result"] == "applied"
assert command("status", {"user_id": user_id, "operation_id": request["operation_id"]}) == result
assert "operation_already_recorded" in command("apply", request, expected=1)
old_client = urllib.request.build_opener(urllib.request.HTTPSHandler(context=context))
old_client.addheaders = [("Cookie", "; ".join(c.name + "=" + c.value for c in jar))]
assert api(old_client, "/api/v1/auth/session")[0] == 401
assert api(old_client, "/api/v1/auth/session", "DELETE", csrf=csrf(jar))[0] == 401
first_stream[0].join(20)
assert not first_stream[0].is_alive() and not first_stream[2], "heartbeat did not close revoked stream"
assert "message.created" not in "".join(first_stream[1]), "unexpected private stream data"
assert api(client, "/api/v1/auth/sessions", "POST", {"email": email, "password": old_password})[0] == 401
browser_pause("after-recovery")
jar, client, current = login(new_password)
assert current["user"] == account["user"] and current["workspaces"] == account["workspaces"]
second_stream = stream(client, workspace_id)
inspection = command("inspect", {"email": email})
request.update(expected_password_changed_at=inspection["password_changed_at"], operation_id=str(uuid.uuid4()), new_password=last_password)
command("apply", request)
final_jar, final_client, final_account = login(last_password)
status, _ = api(final_client, f"/api/v1/workspaces/{workspace_id}/channels/chn_compose/messages", "POST", {"client_operation_id": "compose:recovery:message", "body": "Post-recovery message."}, csrf(final_jar))
assert status == 201
second_stream[0].join(5)
assert not second_stream[0].is_alive() and not second_stream[2], "send recheck did not close revoked stream"
assert "Post-recovery message." not in "".join(second_stream[1]), "revoked stream received new message"
# Let the parent retain its existing explicit logout and post-logout checks.
# urllib represents a host-only localhost cookie as localhost.local; curl's
# Netscape reader requires the actual request host. This is fixture export only.
for cookie in final_jar:
    cookie.domain = urllib.parse.urlsplit(origin).hostname
final_jar.save(str(run_dir / "cookies.txt"), ignore_discard=True, ignore_expires=True)
logs = subprocess.run(compose + ["logs", "--no-color", "app", "caddy"], text=True, capture_output=True, check=True).stdout
for private in private_values + tuple(session_values):
    assert private not in logs, "application or proxy log leaked credential material"
print("Controlled recovery Compose checks passed: CLI, HTTPS, revoked CSRF, heartbeat/send SSE, current permissions.", flush=True)
