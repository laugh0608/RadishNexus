#!/usr/bin/env python3
"""Validate RadishNexus repository-level contracts without third-party packages."""

from __future__ import annotations

import argparse
import json
import re
import subprocess
import sys
from pathlib import Path
from urllib.parse import unquote, urlsplit


REPO_ROOT = Path(__file__).resolve().parents[1]
MAX_PATH_LENGTH = 180
MAX_FILE_BYTES = 10 * 1024 * 1024

REQUIRED_FILES = (
    ".editorconfig",
    ".dockerignore",
    ".gitattributes",
    ".gitignore",
    "AGENTS.md",
    "CLAUDE.md",
    "CODE_OF_CONDUCT.md",
    "CONTRIBUTING.md",
    "LICENSE",
    "README.md",
    "SECURITY.md",
    ".github/ISSUE_TEMPLATE/bug-report.yml",
    ".github/ISSUE_TEMPLATE/change-proposal.yml",
    ".github/ISSUE_TEMPLATE/config.yml",
    ".github/PULL_REQUEST_TEMPLATE.md",
    ".github/rulesets/README.md",
    ".github/rulesets/master-protection.json",
    ".github/workflows/pr-check.yml",
    "docs/README.md",
    "docs/adr/0001-branch-and-pr-governance.md",
    "docs/adr/0016-minimal-docker-compose-self-hosting.md",
    "docs/adr/0017-channel-message-boundary-and-single-process-realtime.md",
    "docs/adr/0020-session-scoped-single-process-message-realtime.md",
    "docs/adr/README.md",
    "docs/development/README.md",
    "docs/development/engineering-standards.md",
    "docs/governance/README.md",
    "docs/governance/agent-collaboration.md",
    "docs/governance/documentation-governance.md",
    "docs/governance/repository-governance.md",
    "deploy/.env.example",
    "deploy/Caddyfile",
    "deploy/Dockerfile",
    "deploy/README.md",
    "deploy/compose.yaml",
    "experiments/m0-core-contracts/README.md",
    "experiments/m0-core-contracts/go.mod",
    "experiments/m0-core-contracts/go.sum",
    "experiments/m0-core-contracts/migrations/001_core_contracts.sql",
    "scripts/README.md",
    "scripts/check-m0-core-contracts-postgres.sh",
    "scripts/check-m0-core-contracts.sh",
    "scripts/check-repo.ps1",
    "scripts/check-repo.sh",
    "scripts/check-self-hosted-compose.sh",
    "scripts/check_repo.py",
    "scripts/tests/test_check_repo.py",
)

TEXT_SUFFIXES = {
    ".bat",
    ".c",
    ".cc",
    ".cmd",
    ".conf",
    ".cpp",
    ".css",
    ".csv",
    ".dart",
    ".env",
    ".go",
    ".graphql",
    ".h",
    ".html",
    ".ini",
    ".js",
    ".json",
    ".jsonc",
    ".jsx",
    ".md",
    ".mjs",
    ".properties",
    ".proto",
    ".ps1",
    ".py",
    ".rs",
    ".scss",
    ".sh",
    ".sql",
    ".svg",
    ".toml",
    ".ts",
    ".tsx",
    ".txt",
    ".xml",
    ".yaml",
    ".yml",
}

TEXT_NAMES = {
    ".editorconfig",
    ".gitattributes",
    ".gitignore",
    "Dockerfile",
    "LICENSE",
    "Makefile",
}

EXCLUDED_DIRECTORIES = {
    ".agents",
    ".claude",
    ".codex",
    ".dart_tool",
    ".git",
    ".idea",
    ".next",
    ".pytest_cache",
    ".venv",
    ".vscode",
    "artifacts",
    "build",
    "coverage",
    "dist",
    "node_modules",
    "target",
    "tmp",
    "var",
    "vendor",
}

COMMIT_SUBJECT = re.compile(
    r"^(?:feat|fix|docs|refactor|test|chore|ci|build|perf|revert)"
    r"(?:\([a-z0-9][a-z0-9._/-]*\))?!?: .+"
)
MARKDOWN_LINK = re.compile(r"!?\[[^\]]*\]\(([^)]+)\)")
LOCAL_PATH_MARKERS = (
    re.compile(r"/Users/[^\s)`>]+"),
    re.compile(r"[A-Za-z]:\\Users\\[^\s)`>]+"),
    re.compile(r"file://[^\s)`>]+", re.IGNORECASE),
    re.compile(r"codex://[^\s)`>]+", re.IGNORECASE),
)


def run_git(*args: str) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        ["git", *args],
        cwd=REPO_ROOT,
        check=False,
        capture_output=True,
        text=True,
    )


def is_git_repository() -> bool:
    return run_git("rev-parse", "--is-inside-work-tree").returncode == 0


def repository_files() -> list[Path]:
    if is_git_repository():
        result = subprocess.run(
            ["git", "ls-files", "--cached", "--others", "--exclude-standard", "-z"],
            cwd=REPO_ROOT,
            check=False,
            capture_output=True,
        )
        if result.returncode == 0:
            paths = [Path(raw.decode("utf-8")) for raw in result.stdout.split(b"\0") if raw]
            return sorted(
                path
                for path in paths
                if not is_excluded(path) and (REPO_ROOT / path).is_file()
            )

    paths: list[Path] = []
    for candidate in REPO_ROOT.rglob("*"):
        relative = candidate.relative_to(REPO_ROOT)
        if candidate.is_file() and not is_excluded(relative):
            paths.append(relative)
    return sorted(paths)


def is_excluded(path: Path) -> bool:
    return any(part in EXCLUDED_DIRECTORIES for part in path.parts)


def is_text_file(path: Path) -> bool:
    return path.name in TEXT_NAMES or path.suffix.lower() in TEXT_SUFFIXES


def is_valid_commit_subject(subject: str) -> bool:
    return COMMIT_SUBJECT.fullmatch(subject) is not None


def markdown_local_target(document: Path, raw_target: str, root: Path) -> Path | None:
    target = raw_target.strip()
    if target.startswith("<") and ">" in target:
        target = target[1 : target.index(">")]
    else:
        target = target.split(maxsplit=1)[0]

    if not target or target.startswith("#"):
        return None

    parsed = urlsplit(target)
    if parsed.scheme or parsed.netloc:
        return None

    decoded_path = unquote(parsed.path)
    if not decoded_path:
        return None
    if decoded_path.startswith("/"):
        raise ValueError("仓库文档应使用相对链接")

    root = root.resolve()
    resolved = (document.parent / decoded_path).resolve()
    try:
        resolved.relative_to(root)
    except ValueError as exc:
        raise ValueError("链接目标逃逸仓库根目录") from exc
    return resolved


def check_required_files(errors: list[str]) -> None:
    for relative in REQUIRED_FILES:
        if not (REPO_ROOT / relative).is_file():
            errors.append(f"缺少必需文件: {relative}")


def check_paths_and_sizes(files: list[Path], errors: list[str]) -> None:
    for relative in files:
        rendered = relative.as_posix()
        if len(rendered) > MAX_PATH_LENGTH:
            errors.append(f"路径超过 {MAX_PATH_LENGTH} 字符: {rendered}")
        size = (REPO_ROOT / relative).stat().st_size
        if size > MAX_FILE_BYTES:
            errors.append(f"文件超过 10 MiB: {rendered} ({size} bytes)")


def check_text_files(files: list[Path], errors: list[str]) -> None:
    for relative in files:
        if not is_text_file(relative):
            continue
        path = REPO_ROOT / relative
        data = path.read_bytes()
        rendered = relative.as_posix()
        if data.startswith(b"\xef\xbb\xbf"):
            errors.append(f"UTF-8 文本不能包含 BOM: {rendered}")
        if b"\r" in data:
            errors.append(f"文本必须使用 LF 换行: {rendered}")
        if data and not data.endswith(b"\n"):
            errors.append(f"文本末尾缺少换行: {rendered}")
        try:
            text = data.decode("utf-8")
        except UnicodeDecodeError as exc:
            errors.append(f"文本不是有效 UTF-8: {rendered}: {exc}")
            continue
        for line_number, line in enumerate(text.splitlines(), start=1):
            if line.endswith((" ", "\t")):
                errors.append(f"行尾空白: {rendered}:{line_number}")


def check_json(files: list[Path], errors: list[str]) -> None:
    for relative in files:
        if relative.suffix.lower() != ".json":
            continue
        try:
            json.loads((REPO_ROOT / relative).read_text(encoding="utf-8"))
        except (OSError, UnicodeDecodeError, json.JSONDecodeError) as exc:
            errors.append(f"JSON 无效: {relative.as_posix()}: {exc}")


def check_markdown(files: list[Path], errors: list[str]) -> None:
    for relative in files:
        if relative.suffix.lower() != ".md":
            continue
        document = REPO_ROOT / relative
        text = document.read_text(encoding="utf-8")
        rendered = relative.as_posix()

        for marker in LOCAL_PATH_MARKERS:
            match = marker.search(text)
            if match:
                errors.append(f"文档包含本机或私有链接: {rendered}: {match.group(0)}")

        for match in MARKDOWN_LINK.finditer(text):
            try:
                target = markdown_local_target(document, match.group(1), REPO_ROOT)
            except ValueError as exc:
                errors.append(f"Markdown 链接无效: {rendered}: {match.group(1)} ({exc})")
                continue
            if target is not None and not target.exists():
                errors.append(
                    f"Markdown 链接目标不存在: {rendered}: {match.group(1)}"
                )


def check_agent_mirrors(errors: list[str]) -> None:
    agents = REPO_ROOT / "AGENTS.md"
    claude = REPO_ROOT / "CLAUDE.md"
    if agents.is_file() and claude.is_file() and agents.read_bytes() != claude.read_bytes():
        errors.append("AGENTS.md 与 CLAUDE.md 必须逐字节一致")


def check_ruleset(errors: list[str]) -> None:
    path = REPO_ROOT / ".github/rulesets/master-protection.json"
    if not path.is_file():
        return
    try:
        ruleset = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, UnicodeDecodeError, json.JSONDecodeError):
        return

    include = ruleset.get("conditions", {}).get("ref_name", {}).get("include")
    if include != ["refs/heads/master"]:
        errors.append("master Ruleset 必须且只能匹配 refs/heads/master")
    if ruleset.get("target") != "branch" or ruleset.get("enforcement") != "active":
        errors.append("master Ruleset 必须是 active branch ruleset")

    rules = {rule.get("type"): rule for rule in ruleset.get("rules", [])}
    for required_type in ("deletion", "non_fast_forward", "pull_request", "required_status_checks"):
        if required_type not in rules:
            errors.append(f"master Ruleset 缺少规则: {required_type}")

    pull_request = rules.get("pull_request", {}).get("parameters", {})
    if pull_request:
        if pull_request.get("allowed_merge_methods") != ["merge", "rebase"]:
            errors.append("master Ruleset 只允许 merge 与 rebase")
        if pull_request.get("required_approving_review_count") != 0:
            errors.append("单维护者阶段 master 最少批准数必须为 0")
        if pull_request.get("require_code_owner_review") is not False:
            errors.append("当前 master Ruleset 不应要求 CODEOWNERS 审查")
        if pull_request.get("require_extra_approval_for_unattributed_changes") is not True:
            errors.append("master Ruleset 必须显式保留未归属 Copilot PR 的默认审批语义")
        if pull_request.get("required_review_thread_resolution") is not True:
            errors.append("master Ruleset 必须要求解决所有审阅对话")

    status = rules.get("required_status_checks", {}).get("parameters", {})
    if status:
        contexts = [item.get("context") for item in status.get("required_status_checks", [])]
        if contexts != ["Candidate Quality"]:
            errors.append("master Ruleset 必需状态必须且只能是 Candidate Quality")
        if status.get("strict_required_status_checks_policy") is not True:
            errors.append("master Ruleset 必须严格要求分支为最新状态")

    bypass = ruleset.get("bypass_actors", [])
    expected_bypass = {
        "actor_id": 5,
        "actor_type": "RepositoryRole",
        "bypass_mode": "pull_request",
    }
    if bypass != [expected_bypass]:
        errors.append("master Ruleset 的管理员绕过必须限制为经 PR")


def check_workflow_contract(errors: list[str]) -> None:
    path = REPO_ROOT / ".github/workflows/pr-check.yml"
    if not path.is_file():
        return
    text = path.read_text(encoding="utf-8")
    required_fragments = (
        "pull_request:",
        "- dev",
        "- master",
        "name: Repo Hygiene",
        "name: Repository Checker Tests",
        "name: M0 Core Contracts",
        "name: Web App",
        "name: Candidate Quality",
        "./scripts/check-repo.sh",
        "./scripts/check-m0-core-contracts.sh",
        "./scripts/check-web.sh",
        "python3 -m unittest discover -s scripts/tests",
        "go test -tags=integration ./...",
        "needs:\n      - repo-hygiene\n      - checker-tests\n      - m0-core-contracts\n      - go-server",
        "- web-app",
    )
    for fragment in required_fragments:
        if fragment not in text:
            errors.append(f"PR Workflow 缺少契约片段: {fragment!r}")


def check_template_contract(errors: list[str]) -> None:
    path = REPO_ROOT / ".github/PULL_REQUEST_TEMPLATE.md"
    if path.is_file():
        text = path.read_text(encoding="utf-8")
        for heading in (
            "## 目标与范围",
            "## 影响面",
            "## 安全、隐私与失败语义",
            "## 验证记录",
            "## 未验证、风险与回滚",
            "## master 合并后回流",
        ):
            if heading not in text:
                errors.append(f"PR 模板缺少章节: {heading}")

    for template in ("bug-report.yml", "change-proposal.yml"):
        issue_path = REPO_ROOT / ".github/ISSUE_TEMPLATE" / template
        if issue_path.is_file():
            content = issue_path.read_text(encoding="utf-8")
            if "name:" not in content or "description:" not in content or "body:" not in content:
                errors.append(f"Issue 模板缺少基本字段: {issue_path.relative_to(REPO_ROOT)}")


def check_git_diff(errors: list[str], base_ref: str | None) -> None:
    if not is_git_repository():
        return
    commands = [("diff", "--check"), ("diff", "--cached", "--check")]
    if base_ref:
        commands = [("diff", "--check", f"{base_ref}...HEAD")]

    for args in commands:
        result = run_git(*args)
        if result.returncode != 0:
            detail = (result.stdout + result.stderr).strip()
            errors.append(f"git {' '.join(args)} 失败: {detail}")


def check_commit_subjects(errors: list[str], base_ref: str | None) -> None:
    if not base_ref or not is_git_repository():
        return
    result = run_git("log", "--format=%H%x09%P%x09%s", f"{base_ref}..HEAD")
    if result.returncode != 0:
        errors.append(f"无法读取待检查提交: {(result.stdout + result.stderr).strip()}")
        return
    for line in result.stdout.splitlines():
        commit, parents, subject = line.split("\t", maxsplit=2)
        if len(parents.split()) > 1:
            continue
        if not is_valid_commit_subject(subject):
            errors.append(f"提交标题不符合 Conventional Commits: {commit[:12]} {subject}")


def parse_args(argv: list[str]) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--base-ref",
        help="只检查相对该 Git ref 的差异和新增提交；CI 通常传入 PR base SHA。",
    )
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv if argv is not None else sys.argv[1:])
    errors: list[str] = []
    files = repository_files()

    check_required_files(errors)
    check_paths_and_sizes(files, errors)
    check_text_files(files, errors)
    check_json(files, errors)
    check_markdown(files, errors)
    check_agent_mirrors(errors)
    check_ruleset(errors)
    check_workflow_contract(errors)
    check_template_contract(errors)
    check_git_diff(errors, args.base_ref)
    check_commit_subjects(errors, args.base_ref)

    if errors:
        print("Repository baseline failed:", file=sys.stderr)
        for error in sorted(set(errors)):
            print(f"- {error}", file=sys.stderr)
        return 1

    print(f"Repository baseline passed ({len(files)} files checked).")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
