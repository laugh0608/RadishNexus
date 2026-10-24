#!/usr/bin/env python3
"""ADR-0043: isolated, offline Compose upgrade and empty-target recovery drill.

Uses local Git artifacts; dependency downloads require an explicit flag. Never accepts a
user database, volume or Compose project. All generated resources belong to this
run. Evidence contains identifiers, counts and timings, never credentials/data.
"""
import argparse
import hashlib
import http.cookiejar
import json
import os
from pathlib import Path
import secrets
import shutil
import signal
import socket
import ssl
import subprocess
import tarfile
import tempfile
import threading
import time
import urllib.error
import urllib.request
import uuid

ROOT = Path(__file__).resolve().parent.parent
POSTGRES = "postgres:17.10-alpine@sha256:742f40ea20b9ff2ff31db5458d127452988a2164df9e17441e191f3b72252193"
CADDY = "caddy:2.11.4-alpine@sha256:5f5c8640aae01df9654968d946d8f1a56c497f1dd5c5cda4cf95ab7c14d58648"
REQUIRED = [POSTGRES, CADDY,
    "golang:1.26.7-alpine3.23@sha256:b17af760035fc2f338eed92d448a6c67f2d45438844fc6c60678fa5f99e44b57",
    "node:24.16.0-alpine3.23@sha256:2bdb65ed1dab192432bc31c95f94155ca5ad7fc1392fb7eb7526ab682fa5bf14",
    "alpine:3.23.5@sha256:fd791d74b68913cbb027c6546007b3f0d3bc45125f797758156952bc2d6daf40"]
PASSWORD = "upgrade-fixture-password-2026"


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def command(args, *, env=None, data=None, expected=0, timeout=600):
    result = subprocess.run(args, input=data, text=True, capture_output=True, env=env, timeout=timeout)
    if expected is not None and result.returncode != expected:
        # Full tool output can contain secrets or SQL data; the caller may inspect
        # it privately, but a report/error never embeds it.
        raise RuntimeError(f"{Path(args[0]).name} {args[1] if len(args)>1 else ''} failed (exit {result.returncode})")
    return result


def free_port():
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


class Drill:
    def __init__(self, options):
        self.options = options
        self.directory = Path(tempfile.mkdtemp(prefix="radishnexus-upgrade-"))
        self.token = self.directory.name.lower()
        self.projects = []
        self.tags = []
        self.private = [PASSWORD]
        self.report = {"status": "running", "artifacts": {}, "stages": [], "browser": {}, "cleanup": "pending"}
        self.started = time.monotonic()

    def stage(self, name, action):
        print(f"Upgrade drill: {name}", flush=True)
        start = time.monotonic()
        result = action()
        self.report["stages"].append({"name": name, "seconds": round(time.monotonic()-start, 3)})
        return result

    def artifact(self, name, ref):
        commit = command(["git", "-C", str(ROOT), "rev-parse", "--verify", ref+"^{commit}"]).stdout.strip()
        directory = self.directory / name
        directory.mkdir()
        archive = self.directory / (name+".tar")
        command(["git", "-C", str(ROOT), "archive", "--format=tar", "--output", str(archive), commit])
        with tarfile.open(archive) as source:
            source.extractall(directory, filter="data")
        archive.unlink()
        images = {}
        for target in ("server", "operations"):
            tag = f"radishnexus/upgrade-{name}-{target}:{self.token}"
            self.tags.append(tag)
            build_env = os.environ.copy()
            build_env["BUILDX_CONFIG"] = str(self.directory/"buildx")
            result = command(["docker", "build", "--network="+("default" if self.options.allow_locked_downloads else "none"), "--pull=false", "--target", target,
                "--tag", tag, "--file", str(directory/"deploy/Dockerfile"), str(directory)], expected=None, env=build_env)
            if result.returncode:
                # Offline cache misses must not silently download dependencies.
                (self.directory/(name+"-build.log")).write_text(result.stdout+result.stderr)
                raise RuntimeError("artifact build failed; private diagnostic: "+str(self.options.report.with_suffix(".build.log")))
            images[target] = command(["docker", "image", "inspect", tag, "--format", "{{.Id}}"]).stdout.strip()
        self.report["artifacts"][name] = {"commit": commit, "images": images, "locked_dependency_downloads_allowed": self.options.allow_locked_downloads}
        return directory, images

    def browser(self, phase, project, ids):
        if not self.options.browser_review:
            self.report["browser"][phase] = "not_run"
            return
        marker = self.directory / ("browser-"+phase+".json")
        marker.write_text(json.dumps({"phase": phase, "origin": project.origin, "ca": str(project.ca), **ids}, ensure_ascii=False))
        print("Browser review barrier: "+str(marker), flush=True)
        deadline = time.monotonic()+1200
        result = marker.with_suffix(".done")
        while not result.exists():
            require(time.monotonic()<deadline, "browser review timed out: "+phase)
            time.sleep(0.25)
        reviewed = json.loads(result.read_text())
        require(reviewed.get("passed") is True and reviewed.get("checks"), "browser review lacks explicit checks")
        self.report["browser"][phase] = reviewed

    def close(self):
        failures = []
        for project in reversed(self.projects):
            try:
                project.run("down", "--volumes", "--remove-orphans")
                remaining = command(["docker", "ps", "-aq", "--filter", "label=com.docker.compose.project="+project.name]).stdout.strip()
                require(not remaining, "owned containers survived cleanup")
                for kind in ("volume","network"):
                    remaining = command(["docker",kind,"ls","-q","--filter","label=com.docker.compose.project="+project.name]).stdout.strip()
                    require(not remaining, "owned "+kind+" survived cleanup")
            except Exception:
                failures.append(project.name)
        # Only remove unique tags created here, never shared cache or user tags.
        for tag in self.tags:
            inspected = command(["docker", "image", "inspect", tag], expected=None)
            if inspected.returncode:
                if "No such image" in inspected.stderr:
                    continue
                failures.append(tag)
                continue
            result = command(["docker", "image", "rm", tag], expected=None)
            if result.returncode:
                failures.append(tag)
        self.report["cleanup"] = "passed" if not failures else {"failed_resources": failures}
        self.report["total_seconds"] = round(time.monotonic()-self.started, 3)
        self.options.report.parent.mkdir(parents=True, exist_ok=True)
        self.options.report.write_text(json.dumps(self.report, indent=2, ensure_ascii=False)+"\n")
        for log in self.directory.glob("*/failure-*.log"):
            diagnostic_dir = self.options.report.with_suffix(".diagnostics")
            diagnostic_dir.mkdir(mode=0o700,exist_ok=True)
            diagnostic = diagnostic_dir/(log.parent.name+"-"+log.name)
            shutil.copyfile(log,diagnostic)
            diagnostic.chmod(0o600)
        if not failures:
            for log in self.directory.glob("*-build.log"):
                diagnostic = self.options.report.with_suffix(".build.log")
                shutil.copyfile(log, diagnostic)
                diagnostic.chmod(0o600)
            shutil.rmtree(self.directory)
        else:
            print("Cleanup incomplete; owned private directory retained: "+str(self.directory), flush=True)
        return not failures


class Project:
    def __init__(self, drill, name, artifact, port=None):
        self.drill = drill
        self.name = drill.token+"-"+name
        self.directory = drill.directory/name
        self.directory.mkdir()
        self.backups = self.directory/"backups"
        self.backups.mkdir()
        self.ca = self.directory/"ca.crt"
        self.port = port or free_port()
        self.origin = f"https://localhost:{self.port}"
        secret = self.directory/"postgres_password"
        password = secrets.token_urlsafe(32)
        secret.write_text(password)
        secret.chmod(0o600)
        drill.private.append(password)
        env = os.environ.copy()
        occupied = command(["docker", "network", "ls", "-q"]).stdout.split()
        subnets = []
        if occupied:
            for network in json.loads(command(["docker", "network", "inspect", *occupied]).stdout):
                subnets.extend(entry.get("Subnet", "") for entry in (network.get("IPAM", {}).get("Config") or []))
        octet = next(i for i in range(20, 240) if f"172.29.{i}.0/24" not in subnets and all(p.octet!=i for p in drill.projects))
        self.octet = octet
        env.update({"RADISHNEXUS_PUBLIC_ORIGIN": self.origin, "RADISHNEXUS_HTTPS_PORT": str(self.port),
            "RADISHNEXUS_POSTGRES_PASSWORD_FILE": str(secret), "RADISHNEXUS_OPERATION_UID": str(os.getuid()),
            "RADISHNEXUS_OPERATION_GID": str(os.getgid()), "RADISHNEXUS_PROXY_SUBNET": f"172.29.{octet}.0/24",
            "RADISHNEXUS_PROXY_IPV4": f"172.29.{octet}.10", "RADISHNEXUS_TRUSTED_PROXY_CIDR": f"172.29.{octet}.10/32"})
        source, images = artifact
        config = json.loads(command(["docker", "compose", "-p", self.name, "-f", str(source/"deploy/compose.yaml"), "--profile", "operations", "config", "--format", "json"], env=env).stdout)
        self.file = self.directory/"compose.json"
        self.config = config
        for service, value in config["services"].items():
            value.pop("build", None)
            if service=="app":
                value["image"] = images["server"]
            elif service not in ("postgres", "caddy"):
                value["image"] = images["operations"]
            if service in ("backup", "restore"):
                value["volumes"] = [{"type": "bind", "source": str(self.backups), "target": "/backups", "read_only": service=="restore"}]
            if service=="caddy":
                for mapping in value["ports"]:
                    mapping["host_ip"] = "127.0.0.1"
        self.save()
        drill.projects.append(self)

    def save(self):
        self.file.write_text(json.dumps(self.config))

    def set_artifact(self, artifact):
        source, images = artifact
        for name, value in self.config["services"].items():
            if name=="app": value["image"] = images["server"]
            elif name not in ("postgres", "caddy"): value["image"] = images["operations"]
        # Both historical configurations must also match their artifacts.
        for volume in self.config["services"]["caddy"]["volumes"]:
            if volume["target"]=="/etc/caddy/Caddyfile": volume["source"] = str(source/"deploy/Caddyfile")
        self.save()

    def run(self, *args, data=None, expected=0, timeout=600):
        result = command(["docker", "compose", "-p", self.name, "-f", str(self.file), *args], data=data, expected=None, timeout=timeout)
        if expected is not None and result.returncode!=expected:
            diagnostic = self.directory/("failure-"+uuid.uuid4().hex+".log")
            diagnostic.write_text(result.stdout+result.stderr)
            diagnostic.chmod(0o600)
            raise RuntimeError(f"Compose {self.name} {args[0]} failed (exit {result.returncode}); private diagnostics: {self.drill.options.report.with_suffix('.diagnostics')}")
        return result

    def op(self, name, *args, data=None, expected=0):
        return self.run("run", "--rm", "--no-deps", "-T", name, *args, data=data, expected=expected)

    def sql(self, sql):
        return self.run("exec", "-T", "postgres", "psql", "-X", "-A", "-t", "-v", "ON_ERROR_STOP=1", "-U", "radishnexus", "-d", "radishnexus", data=sql).stdout.strip()

    def start(self):
        self.run("up", "-d", "--no-build", "--pull", "never", "--wait", "app", "caddy")
        self.run("cp", "caddy:/data/caddy/pki/authorities/local/root.crt", str(self.ca))
        probe = Client(self)
        require(probe.request("/health/ready")[0]==204, "matched artifact not ready")

    def stop(self):
        self.run("stop", "caddy", "app")
        self.assert_stopped()
        self.drill.report.setdefault("stopped_targets", []).append(self.name)

    def assert_stopped(self):
        running = self.run("ps", "--status", "running", "-q", "app", "caddy").stdout.strip()
        require(not running, "application writer still running")
        require(self.sql("SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND backend_type='client backend'")=="0", "unaccounted database client remains")

    def identity(self, expected=None):
        container = self.run("ps", "-q", "postgres").stdout.strip()
        info = json.loads(command(["docker", "container", "inspect", container]).stdout)[0]
        require(info["Config"]["Labels"].get("com.docker.compose.project")==self.name, "wrong project identity")
        require(not info["NetworkSettings"]["Ports"].get("5432/tcp"), "database published to host")
        mounts = [m["Name"] for m in info["Mounts"] if m["Destination"]=="/var/lib/postgresql/data"]
        require(len(mounts)==1 and mounts[0].startswith(self.name+"_"), "wrong database volume identity")
        result = {"project": self.name, "container": container, "volume": mounts[0], "system_identifier": self.sql("SELECT system_identifier FROM pg_control_system()")}
        require(expected is None or result==expected, "target does not match the independently recorded instance identity")
        return result

    def snapshot(self, tables, columns=None):
        result, shape = {}, {}
        for table in tables:
            schema, name = table.split(".")
            require(schema in ("public", "radishnexus") and name.replace("_", "").isalnum(), "invalid manifest table")
            fields = columns[table] if columns else self.sql(f"SELECT string_agg(column_name,',' ORDER BY ordinal_position) FROM information_schema.columns WHERE table_schema='{schema}' AND table_name='{name}'").split(",")
            require(all(f.replace("_", "").isalnum() for f in fields), "invalid snapshot column")
            shape[table] = fields
            raw = self.sql(f"SELECT coalesce(jsonb_agg(to_jsonb(r) ORDER BY to_jsonb(r)::text),'[]')::text FROM (SELECT {','.join(fields)} FROM {table}) r")
            result[table] = hashlib.sha256(raw.encode()).hexdigest()
        return result, shape


class Client:
    def __init__(self, project):
        self.project = project
        self.jar = http.cookiejar.CookieJar()
        self.context = ssl.create_default_context(cafile=str(project.ca))
        self.opener = urllib.request.build_opener(urllib.request.HTTPSHandler(context=self.context), urllib.request.HTTPCookieProcessor(self.jar))

    def request(self, path, method="GET", body=None, retry_rate_limit=True):
        headers = {"Origin": self.project.origin}
        if method!="GET":
            for cookie in self.jar:
                if cookie.name=="__Host-radishnexus-csrf": headers["X-CSRF-Token"] = cookie.value
        data = None
        if body is not None:
            for key in ("email","display_name","name","title","question","outcome","rationale","body","body_markdown"):
                value = body.get(key)
                if isinstance(value,str) and value and value not in self.project.drill.private:
                    self.project.drill.private.append(value)
            data = json.dumps(body).encode()
            headers["Content-Type"] = "application/json"
        try:
            response = self.opener.open(urllib.request.Request(self.project.origin+path, data=data, method=method, headers=headers), timeout=20)
        except urllib.error.HTTPError as error:
            response = error
        if response.status==429 and retry_rate_limit:
            delay = min(61,max(1,int(response.headers.get("Retry-After","60"))))
            response.close()
            print("Upgrade drill: waiting for the unchanged authentication rate limit",flush=True)
            time.sleep(delay+1)
            return self.request(path,method,body,retry_rate_limit=False)
        with response:
            raw = response.read()
            for cookie in self.jar:
                if cookie.value not in self.project.drill.private: self.project.drill.private.append(cookie.value)
            value = json.loads(raw) if raw else None
            if response.status==404 and path.startswith("/api/"):
                request_id = response.headers.get("X-Request-ID")
                require(request_id and value["error"]["request_id"]==request_id,"error request ID missing or inconsistent")
                self.project.drill.report.setdefault("diagnostic_request_ids", {})[self.project.name] = request_id
            return response.status, value

    def api(self, path, method="GET", body=None, status=200):
        actual, value = self.request(path, method, body)
        require(actual==status, f"formal API {method} {path} returned {actual}, expected {status}")
        return value

    def login(self, email):
        return self.api("/api/v1/auth/sessions", "POST", {"email": email, "password": PASSWORD}, 201)

    def write(self, path, body, status=201, method="POST"):
        return self.api(path, method, {"client_operation_id": str(uuid.uuid4()), **body}, status)["data"]


def fixture(project):
    project.run("up", "-d", "--no-build", "--pull", "never", "--wait", "postgres")
    project.op("migrate")
    project.op("bootstrap", "--display-name", "升级演练管理员", "--workspace-name", "升级恢复演练", "--credentials-stdin", data=json.dumps({"email":"owner@example.test", "password":PASSWORD}))
    project.start()
    owner, member, outsider = Client(project), Client(project), Client(project)
    account = owner.login("owner@example.test")
    workspace, actor = account["workspaces"][0]["id"], account["user"]["id"]
    base = "/api/v1/workspaces/"+workspace
    members = []
    for email, client in (("member@example.test", member), ("outsider@example.test", outsider)):
        invite = owner.api(base+"/invitations", "POST", {}, 201)["invitation_token"]
        project.drill.private.append(invite)
        client.api("/api/v1/auth/invitations/accept", "POST", {"invitation_token":invite,"email":email,"password":PASSWORD,"display_name":"演练成员"},201)
        members.append(client.login(email)["user"]["id"])
    # Retain an unconsumed pre-upgrade invitation to verify tenure migration.
    invitation = owner.api(base+"/invitations", "POST", {}, 201)["invitation_token"]
    project.drill.private.append(invitation)
    team = owner.write(base+"/teams", {"name":"恢复演练团队"})["id"]
    prj = owner.write(base+"/projects", {"key":"upgrade","name":"升级恢复项目","owner_team_id":team,"visibility":"restricted","initial_admin_user_id":actor})["ref"]["id"]
    owner.write(base+f"/projects/{prj}/members/{members[0]}", {"role":"contributor","expected_role":None},200,"PUT")
    channel = owner.write(base+f"/projects/{prj}/channels", {"name":"维护协作","visibility":"project","member_user_ids":[]})["ref"]["id"]
    message = member.write(base+f"/channels/{channel}/messages", {"body":"升级前已确认的协作事实"})["ref"]["id"]
    thread = member.api(base+f"/channels/{channel}/messages/{message}/threads", "POST", {"title":"恢复验证讨论","visibility":"project"},201)["data"]["ref"]["id"]
    decision = member.write(base+f"/threads/{thread}/decisions", {"question":"如何验证升级失败后上下文仍完整？"})["decision"]["ref"]["id"]
    owner.write(base+f"/decisions/{decision}/acceptance", {"outcome":"停写后备份，空目标恢复并复验","rationale":"保持来源和权限语义","confirmed":True},200)
    ticket = member.write(base+f"/decisions/{decision}/tickets", {"title":"验证升级与恢复闭环"})["ticket"]["ref"]["id"]
    member.write(base+f"/tickets/{ticket}/status-transitions", {"base_status_revision":1,"to_status":"in-progress","confirmed":True},200)
    document = member.write(base+f"/tickets/{ticket}/documents", {"title":"恢复验收记录","body_markdown":"# 升级前记录\n\n来源与版本需要保留。","format_version":"nexus-markdown-v1"})["ref"]["id"]
    member.write(base+f"/documents/{document}/revisions", {"title":"恢复验收记录","body_markdown":"# 升级前记录\n\n第二版：停写、验证备份、显式迁移。","format_version":"nexus-markdown-v1","base_revision":1},200)
    ids = {"workspace":workspace,"project":prj,"channel":channel,"thread":thread,"decision":decision,"ticket":ticket,"document":document,"owner":actor,"member":members[0]}
    verify_paths(project, member, outsider, ids)
    return ids, owner, member, outsider


def verify_paths(project, member, outsider, ids):
    base = "/api/v1/workspaces/"+ids["workspace"]
    for path in (f"/projects/{ids['project']}/tickets",f"/projects/{ids['project']}/decisions",f"/projects/{ids['project']}/documents",f"/channels/{ids['channel']}/messages", f"/threads/{ids['thread']}/nexus-view", f"/decisions/{ids['decision']}/nexus-view",f"/tickets/{ids['ticket']}/nexus-view",f"/documents/{ids['document']}/nexus-view"):
        member.api(base+path)
        require(outsider.request(base+path)[0]==404, "restricted object leaked to unrelated member")
    ticket = member.api(base+f"/tickets/{ids['ticket']}/nexus-view")["data"]
    require(ticket["current"]["status"]=="in-progress" and ticket["current"]["status_revision"]==2, "Ticket state lost")
    require(ticket["relations"] and ticket["timeline"], "Ticket provenance or Activity lost")
    doc = member.api(base+f"/documents/{ids['document']}/nexus-view")["data"]
    require(doc["current"]["revision"]==2 and doc["timeline"], "Document history lost")
    # An exact retry must return the original operation without adding a fact.
    request = {"client_operation_id":"upgrade-drill-repeat","body":"幂等重试验证"}
    status, first = member.request(base+f"/channels/{ids['channel']}/messages","POST",request)
    require(status in (200,201),"message creation or historical replay rejected")
    second = member.api(base+f"/channels/{ids['channel']}/messages","POST",request,200)
    require(first==second,"message exact retry drift")


def sse_start(member, ids):
    ready, closed = threading.Event(), threading.Event()
    path = f"/api/v1/workspaces/{ids['workspace']}/channels/{ids['channel']}/events"
    errors = []
    def read():
        try:
            with member.opener.open(member.project.origin+path, timeout=60) as response:
                for line in response:
                    if line.strip()==b"event: ready": ready.set()
        except Exception as error:
            if not ready.is_set(): errors.append(type(error).__name__)
        finally:
            closed.set()
    worker = threading.Thread(target=read, daemon=True)
    worker.start()
    require(ready.wait(10) and not errors and not closed.is_set(), "SSE did not remain open after ready")
    return closed, worker



def refused_action(action, expected):
    try:
        action()
    except RuntimeError as error:
        require(expected in str(error), "negative check failed at an unexpected boundary")
        return
    raise RuntimeError("negative check unexpectedly succeeded")


def locked_migration_check(source):
    # The only extra writer is explicitly owned by this drill. Its backend PID
    # is discovered inside this project's database, not by a broad process kill.
    args = ["docker","compose","-p",source.name,"-f",str(source.file),"exec","-T","postgres",
        "psql","-X","-v","ON_ERROR_STOP=1","-U","radishnexus","-d","radishnexus"]
    lock = subprocess.Popen(args,stdin=subprocess.PIPE,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,text=True)
    lock.stdin.write("SET application_name='upgrade-drill-lock'; SELECT pg_advisory_lock(739140967704144); SELECT pg_sleep(180);\n")
    lock.stdin.close()
    pid = None
    try:
        deadline = time.monotonic()+10
        while time.monotonic()<deadline:
            value = source.sql("SELECT a.pid FROM pg_stat_activity a JOIN pg_locks l ON l.pid=a.pid WHERE a.application_name='upgrade-drill-lock' AND l.locktype='advisory' AND l.granted")
            if value:
                require(value.isdigit(),"ambiguous test lock owner")
                pid = int(value)
                break
            time.sleep(0.1)
        require(pid is not None,"test lock was not acquired")
        refused_action(source.assert_stopped,"unaccounted database client")
        before = source.sql("SELECT jsonb_agg(to_jsonb(h) ORDER BY sequence) FROM public.radishnexus_schema_migrations h")
        started = time.monotonic()
        result = source.op("migrate",expected=None)
        elapsed = time.monotonic()-started
        require(result.returncode!=0 and "acquire migration advisory lock" in result.stderr,"real CLI did not report lock-budget failure")
        require(115 < elapsed < 150,"unexpected CLI request timeout observation")
        after = source.sql("SELECT jsonb_agg(to_jsonb(h) ORDER BY sequence) FROM public.radishnexus_schema_migrations h")
        require(before==after,"timed-out waiter changed history")
        source.drill.report["cli_lock_budget_seconds"] = round(elapsed,3)
    finally:
        if pid is not None:
            source.sql(f"SELECT pg_terminate_backend({pid})")
        lock.wait(timeout=10)
    source.assert_stopped()



def app_failure_checks(source, old, new):
    source.assert_stopped()
    source.set_artifact(old)
    source.run("up","-d","--no-build","--pull","never","--no-deps","app")
    deadline = time.monotonic()+15
    while True:
        live = source.run("exec","-T","app","wget","-q","-O","/dev/null","http://127.0.0.1:8080/health/live",expected=None)
        if live.returncode==0: break
        require(time.monotonic()<deadline,"old app never became live")
        time.sleep(0.1)
    ready = source.run("exec","-T","app","wget","-S","-O","/dev/null","http://127.0.0.1:8080/health/ready",expected=None)
    require(ready.returncode!=0 and "503" in ready.stderr,"live old app did not reject new schema")
    source.run("stop","app")
    source.set_artifact(new)
    app = source.config["services"]["app"]
    previous = app["environment"].get("RADISHNEXUS_WEB_ROOT")
    restart = app["restart"]
    app["environment"]["RADISHNEXUS_WEB_ROOT"] = "/srv/radishnexus/absent-upgrade-web"
    app["restart"] = "no"
    source.save()
    try:
        source.run("up","-d","--no-build","--pull","never","--no-deps","app")
        source.run("wait","app",expected=None,timeout=30)
        container = source.run("ps","--all","-q","app").stdout.strip()
        exit_code = command(["docker","inspect","--format","{{.State.ExitCode}}",container]).stdout.strip()
        require(exit_code!="0","missing Web build did not fail startup")
        source.assert_stopped()
    finally:
        source.run("stop","app")
        app["restart"] = restart
        if previous is None: del app["environment"]["RADISHNEXUS_WEB_ROOT"]
        else: app["environment"]["RADISHNEXUS_WEB_ROOT"] = previous
        source.save()

def backup_failure_checks(source, target):
    backup = target.backups/"before-upgrade"
    intact = hashlib.sha256((backup/"database.dump").read_bytes()).hexdigest()
    secret = Path(source.config["secrets"]["postgres_password"]["file"])
    held_secret = secret.with_suffix(".unavailable")
    secret.rename(held_secret)
    try:
        result = source.op("backup","--output","/backups/missing-secret",expected=None)
        require(result.returncode!=0,"operation started without required Secret file")
        require(not (source.backups/"missing-secret").exists(),"missing-secret operation published backup")
    finally:
        held_secret.rename(secret)
    held_ca = source.ca.with_suffix(".unavailable")
    source.ca.rename(held_ca)
    try:
        try:
            Client(source)
        except FileNotFoundError:
            pass
        else:
            raise RuntimeError("HTTPS client accepted missing CA material")
    finally:
        held_ca.rename(source.ca)
    for variant in ("missing","corrupt"):
        path = target.backups/variant
        if variant=="corrupt":
            shutil.copytree(backup,path)
            with (path/"database.dump").open("ab") as output: output.write(b"isolated corruption")
        result = target.op("restore","--input","/backups/"+variant,expected=None)
        require(result.returncode!=0,"invalid backup accepted")
        require(target.sql("SELECT to_regclass('public.radishnexus_schema_migrations') IS NULL")=="t","invalid backup wrote target")
    # Fill only an operation container's own 16MiB tmpfs, never the host volume.
    result = source.run("run","--rm","--no-deps","-T","--entrypoint","sh","backup","-ec",
        "dd if=/dev/zero of=/tmp/upgrade-fill bs=1048576 count=32 2>/dev/null || test -f /tmp/upgrade-fill; /usr/local/bin/nexus-backup --output /tmp/upgrade-backup",expected=None)
    require(result.returncode!=0 and "no space left" in (result.stdout+result.stderr).lower(),"temporary disk-full fault not observed")
    require(hashlib.sha256((backup/"database.dump").read_bytes()).hexdigest()==intact,"valid backup damaged by negative checks")
    require(target.sql("SELECT to_regclass('public.radishnexus_schema_migrations') IS NULL")=="t","preflight faults changed target")

def drill_run(drill):
    for image in REQUIRED:
        command(["docker", "image", "inspect", image])
    drill.report["host"] = {"platform":os.uname().sysname,"architecture":os.uname().machine,
        "docker":command(["docker","version","--format","{{.Server.Version}}"]).stdout.strip(),
        "compose":command(["docker","compose","version","--short"]).stdout.strip()}
    drill.report["host"]["docker_resources"] = json.loads(command(["docker","info","--format",'{"cpus":{{.NCPU}},"memory_bytes":{{.MemTotal}}}']).stdout)
    old = drill.stage("build fixed local schema 017 artifacts",lambda:drill.artifact("old",drill.options.old_ref))
    new = drill.stage("build fixed local schema 018 artifacts",lambda:drill.artifact("new",drill.options.new_ref))
    source = Project(drill,"source",old)
    ids, owner, member, outsider = drill.stage("formal 017 collaboration",lambda:fixture(source))
    drill.private.extend(source.sql("SELECT password_hash FROM radishnexus.local_credentials").splitlines())
    drill.report["source"] = source.identity()
    drill.browser("before-upgrade",source,ids)
    closed, worker = sse_start(member,ids)
    drill.stage("stop ingress and writers",source.stop)
    require(closed.wait(10), "SSE survived explicit ingress stop")
    worker.join(timeout=1)
    drill.stage("verified stopped-source backup",lambda:source.op("backup","--output","/backups/before-upgrade"))
    manifest = json.loads((source.backups/"before-upgrade/manifest.json").read_text())
    require(len(manifest["migrations"])==17,"source artifact is not 017")
    tables = manifest["included_data_tables"]
    baseline, columns = source.snapshot(tables)
    activity, _ = source.snapshot(["radishnexus.activity_items"])
    drill.report["data"] = {"dump_bytes":manifest["database_dump"]["size_bytes"],"authority_tables":len(tables),
        "database_bytes":int(source.sql("SELECT pg_database_size(current_database())")),
        "entities":{table:int(source.sql("SELECT count(*) FROM radishnexus."+table)) for table in ("messages","decisions","tickets","documents","document_revisions","domain_events")}}
    target = Project(drill,"restored",old,source.port)
    target.run("up","-d","--no-build","--pull","never","--wait","postgres")
    drill.report["restored"] = target.identity()
    require(drill.report["restored"]["system_identifier"]!=drill.report["source"]["system_identifier"],"source and restored database identity collide")
    shutil.copytree(source.backups/"before-upgrade",target.backups/"before-upgrade")
    # A new tool must reject an old backup before creating schema.
    target.set_artifact(new)
    refused = target.op("restore","--input","/backups/before-upgrade",expected=None)
    require(refused.returncode!=0 and "migration history" in refused.stderr,"new tool accepted old backup")
    require(target.sql("SELECT to_regclass('public.radishnexus_schema_migrations') IS NULL")=="t","mismatch wrote target")
    target.set_artifact(old)
    refused_action(lambda:target.identity(drill.report["source"]),"independently recorded instance identity")
    drill.stage("missing corrupt and disk-full backup refusal",lambda:backup_failure_checks(source,target))
    drill.stage("017 backup round trip into independent empty target",lambda:target.op("restore","--input","/backups/before-upgrade"))
    restored,_ = target.snapshot(tables)
    require(restored==baseline,"old backup authority mismatch")
    restored_activity,_ = target.snapshot(["radishnexus.activity_items"])
    require(activity==restored_activity,"Activity rebuild mismatch")
    # Wrong mapping is caught by independently comparing container/volume/system
    # identities; matching Workspace/user IDs are deliberately insufficient.
    require(target.identity()!=source.identity(),"recovery target aliases source")
    source.set_artifact(new)
    drill.stage("real CLI lock timeout and unexpected writer refusal",lambda:locked_migration_check(source))
    drill.stage("explicit 017 to 018 migration",lambda:source.op("migrate"))
    require(source.sql("SELECT max(sequence) FROM public.radishnexus_schema_migrations")=="18","018 not committed")
    require(source.sql("SELECT bool_and(role_revision=1) FROM radishnexus.workspace_memberships")=="t","role revisions not initialized")
    require(source.sql("SELECT bool_and(creator_role_revision=1) FROM radishnexus.identity_invitations WHERE consumed_at IS NULL")=="t","invitation tenure lost")
    upgraded,_ = source.snapshot([t for t in tables if t!="public.radishnexus_schema_migrations"],columns)
    require(upgraded=={k:v for k,v in baseline.items() if k!="public.radishnexus_schema_migrations"},"upgrade changed old authoritative facts")
    drill.stage("old app schema refusal and missing Web startup failure",lambda:app_failure_checks(source,old,new))
    drill.stage("matched 018 service and member verification",source.start)
    require(member.request("/api/v1/auth/session")[0]==200,"ordinary upgrade incorrectly revoked all sessions")
    verify_paths(source,member,outsider,ids)
    drill.browser("after-upgrade",source,ids)
    # Backup-point loss is deliberately visible. This is disposable test data;
    # real post-backup writes require a fresh operator data-loss decision.
    base = "/api/v1/workspaces/"+ids["workspace"]
    lost_message = member.write(base+f"/channels/{ids['channel']}/messages",{"body":"备份后产生、恢复时明确舍弃的演练事实"})["ref"]["id"]
    ownership = owner.api(base+"/ownership")["data"]["self"]
    candidate = next(x for x in owner.api(base+"/owner-transfer-candidates")["data"]["items"] if x["id"]==ids["member"])
    transfer_id = str(uuid.uuid4())
    owner.api(base+"/owner-transfers","POST",{"client_operation_id":transfer_id,"target_user_id":ids["member"],"expected_actor_role_revision":ownership["role_revision"],"expected_target_role_revision":candidate["role_revision"],"confirmed":True})
    drill.report["discarded_test_facts"] = {"message":lost_message,"owner_transfer_operation":transfer_id}
    drill.stage("stop upgraded source before restoring old service",source.stop)
    source_after,_ = source.snapshot(tables)
    drill.stage("open only the matched restored 017 service",target.start)
    # Same origin/IDs, distinct database. Original cookies cannot authenticate.
    prior = Client(target)
    prior.jar = member.jar
    prior.opener = urllib.request.build_opener(urllib.request.HTTPSHandler(context=prior.context), urllib.request.HTTPCookieProcessor(prior.jar))
    require(prior.request("/api/v1/auth/session")[0]==401,"old session revived in restored instance")
    member2, outsider2, owner2 = Client(target),Client(target),Client(target)
    member2.login("member@example.test")
    outsider2.login("outsider@example.test")
    require(owner2.login("owner@example.test")["workspaces"][0]["role"]=="owner","old owner snapshot not restored")
    require(owner2.api("/api/v1/setup")["status"]=="complete","restore reopened setup")
    require(target.sql("SELECT count(*) FROM radishnexus.identity_invitations")=="0","old invitation restored")
    require(target.sql("SELECT count(*) FROM radishnexus.oidc_transactions")=="0","OIDC transaction restored")
    require(target.sql(f"SELECT count(*) FROM radishnexus.messages WHERE id='{lost_message}'")=="0","post-backup message silently replayed")
    verify_paths(target,member2,outsider2,ids)
    require(target.sql("SELECT max(sequence) FROM public.radishnexus_schema_migrations")=="17","old recovery silently upgraded")
    drill.browser("restored-old",target,ids)
    # Separately exercise the explicit decision to upgrade a restored copy.
    drill.stage("stop restored copy for separately selected upgrade",target.stop)
    target.set_artifact(new)
    drill.stage("explicitly upgrade restored copy to 018",lambda:target.op("migrate"))
    target.start()
    verify_paths(target,member2,outsider2,ids)
    history = owner2.api(base+"/owner-transfers/"+transfer_id)["data"]
    require(history["result"]=="not_observed","post-backup transfer was replayed")
    require(owner2.api("/api/v1/auth/session")["workspaces"][0]["role"]=="owner","missing receipt changed current role")
    drill.browser("restored-upgraded",target,ids)
    after,_ = source.snapshot(tables)
    require(after==source_after,"recovery or target upgrade modified failed source")
    # Scan privately held output; never publish raw request/cookie/verifier data.
    for project in (source,target):
        logs = project.run("logs","--no-color","app","caddy","postgres").stdout
        for value in drill.private:
            require(value not in logs,"sensitive fixture material entered logs")
    drill.report["status"] = "passed"
    drill.report["operator_review"] = "pending independent operator walkthrough; automation is not operator acceptance"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--old-ref",default="333c4ce")
    parser.add_argument("--new-ref",default="171527f")
    parser.add_argument("--report",type=Path,required=True,help="new redacted JSON evidence path, outside the disposable run directory")
    parser.add_argument("--browser-review",action="store_true")
    parser.add_argument("--allow-locked-downloads",action="store_true",help="explicitly authorize official Dockerfile dependency downloads at existing locked versions")
    options = parser.parse_args()
    def interrupt(_signum, _frame):
        raise KeyboardInterrupt("upgrade drill interrupted")
    signal.signal(signal.SIGTERM, interrupt)
    require(not options.report.exists(),"report already exists; choose a new path")
    drill = Drill(options)
    okay = False
    try:
        drill_run(drill)
        okay = True
    except (Exception, KeyboardInterrupt) as error:
        drill.report["status"] = "failed"
        drill.report["failure"] = str(error)
        print("Upgrade drill failed: "+str(error),flush=True)
    finally:
        clean = drill.close()
    print("Redacted upgrade evidence: "+str(options.report),flush=True)
    return 0 if okay and clean else 1


if __name__=="__main__":
    raise SystemExit(main())
