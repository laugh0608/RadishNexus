#!/usr/bin/env python3
"""ADR-0042 HTTPS checks within the disposable self-hosted Compose project.

Only synthetic identities are created through formal invitation and login APIs.
The parent owns the private directory and all resource cleanup.
"""
import http.cookiejar
import json
import os
from pathlib import Path
import ssl
import subprocess
import sys
import time
import urllib.error
import urllib.request
import uuid

run_dir = Path(sys.argv[1])
compose = json.loads(sys.argv[2])
origin = os.environ["RADISHNEXUS_PUBLIC_ORIGIN"]
context = ssl.create_default_context(cafile=str(run_dir / "caddy-root.crt"))
private = ["admin@example.test", "compose-final-password-2026", "owner-transfer-member-password-2026"]


def client():
    jar = http.cookiejar.CookieJar()
    opener = urllib.request.build_opener(urllib.request.HTTPSHandler(context=context), urllib.request.HTTPCookieProcessor(jar))
    return jar, opener


def api(pair, path, method="GET", body=None, retry=True):
    jar, opener = pair
    headers = {"Origin": origin}
    data = None
    if body is not None:
        data = json.dumps(body).encode()
        headers["Content-Type"] = "application/json"
    if method != "GET":
        for cookie in jar:
            if cookie.name == "__Host-radishnexus-csrf":
                headers["X-CSRF-Token"] = cookie.value
    try:
        response = opener.open(urllib.request.Request(origin + path, data=data, method=method, headers=headers), timeout=25)
    except urllib.error.HTTPError as error:
        response = error
    if response.status == 429 and retry:
        delay = min(61, max(1, int(response.headers.get("Retry-After", "60"))))
        response.close()
        print("Waiting for the unchanged authentication rate limit.", flush=True)
        time.sleep(delay + 1)
        return api(pair, path, method, body, retry=False)
    with response:
        raw = response.read()
        for cookie in jar:
            if cookie.value not in private:
                private.append(cookie.value)
        return response.status, json.loads(raw) if raw else None


def login(email, password):
    pair = client()
    status, account = api(pair, "/api/v1/auth/sessions", "POST", {"email": email, "password": password})
    assert status == 201, "formal handoff login failed"
    return pair, account


a, account = login("admin@example.test", "compose-final-password-2026")
workspace = account["workspaces"][0]["id"]
actor = account["user"]["id"]
base = f"/api/v1/workspaces/{workspace}"
members = []
for n in range(2):
    status, invite = api(a, base + "/invitations", "POST", {})
    assert status == 201
    private.append(invite["invitation_token"])
    email = f"handoff-{n}@example.test"
    private.append(email)
    status, joined = api(client(), "/api/v1/auth/invitations/accept", "POST", {"invitation_token": invite["invitation_token"], "email": email, "password": "owner-transfer-member-password-2026", "display_name": "同名接任者"})
    assert status == 201
    pair, current = login(email, "owner-transfer-member-password-2026")
    assert current["workspaces"][0]["role"] == "member"
    members.append((pair, current["user"]["id"]))
status, old_invite = api(a, base + "/invitations", "POST", {})
assert status == 201
private.append(old_invite["invitation_token"])

if os.environ.get("RADISHNEXUS_OWNER_TRANSFER_BROWSER_REVIEW") == "true":
    marker = run_dir / "browser-owner-transfer.json"
    marker.write_text(json.dumps({"origin": origin, "workspace_id": workspace, "actor_id": actor, "member_ids": [member[1] for member in members]}))
    print("Owner transfer browser barrier: " + str(marker), flush=True)
    deadline = time.monotonic() + 1200
    while not marker.with_suffix(".done").exists():
        assert time.monotonic() < deadline, "owner transfer browser deadline exceeded"
        time.sleep(0.2)

b, target = members[0]
status, ownership = api(a, base + "/ownership")
assert status == 200 and ownership["data"]["self"]["role"] == "owner", "browser review must return ownership to original actor"
status, candidates = api(a, base + "/owner-transfer-candidates")
assert status == 200 and len(candidates["data"]["items"]) == 2
assert "email" not in json.dumps(candidates)
candidate = next(row for row in candidates["data"]["items"] if row["id"] == target)
request = {"client_operation_id": str(uuid.uuid4()), "target_user_id": target, "expected_actor_role_revision": ownership["data"]["self"]["role_revision"], "expected_target_role_revision": candidate["role_revision"], "confirmed": True}
status, result = api(a, base + "/owner-transfers", "POST", request)
assert status == 200 and result["data"]["result"] == "applied"
assert api(a, base + "/owner-transfers", "POST", request) == (200, result)
assert api(a, base + "/owner-transfers/" + request["client_operation_id"]) == (200, result)
assert api(a, base + "/owner-transfer-candidates")[0] == 403
assert api(a, base + "/invitations", "POST", {})[0] == 403
assert api(a, base + "/teams", "POST", {"client_operation_id": "forbidden-owner-team", "name": "Forbidden"})[0] == 403
assert api(a, "/api/v1/auth/session")[1]["workspaces"][0]["role"] == "member"
assert api(b, "/api/v1/auth/session")[1]["workspaces"][0]["role"] == "owner"
assert api(client(), "/api/v1/auth/invitations/accept", "POST", {"invitation_token": old_invite["invitation_token"], "email": "stale-handoff@example.test", "password": "owner-transfer-member-password-2026", "display_name": "Stale"})[0] == 403
assert api(b, base + "/teams", "POST", {"client_operation_id": "new-owner-team", "name": "New owner team"})[0] == 201
# Transfer onward; A's historical result must remain unchanged and private.
c, last = members[1]
b_owner = api(b, base + "/ownership")[1]["data"]
candidates = api(b, base + "/owner-transfer-candidates")[1]["data"]["items"]
next_target = next(row for row in candidates if row["id"] == last)
next_request = {"client_operation_id": str(uuid.uuid4()), "target_user_id": last, "expected_actor_role_revision": b_owner["self"]["role_revision"], "expected_target_role_revision": next_target["role_revision"], "confirmed": True}
assert api(b, base + "/owner-transfers", "POST", next_request)[0] == 200
assert api(a, base + "/owner-transfers/" + request["client_operation_id"]) == (200, result)
assert api(c, base + "/owner-transfers/" + request["client_operation_id"])[1]["data"]["result"] == "not_observed"
logs = subprocess.run(compose + ["logs", "--no-color", "app", "caddy"], text=True, capture_output=True, check=True).stdout
for value in private:
    assert value not in logs, "handoff logs leaked private fixture material"
print("Owner transfer Compose checks passed: formal identity, HTTPS, live roles, immutable scoped retry, invitation tenure and private logs.", flush=True)
