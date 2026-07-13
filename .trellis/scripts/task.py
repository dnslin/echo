#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Task Management Script.

Usage:
    python3 task.py create "<title>" [--slug <name>] [--assignee <dev>] [--priority P0|P1|P2|P3] [--parent <dir>] [--package <pkg>] [--complex] [--no-start]
    python3 task.py add-context <dir> <file> <path> [reason] # Add jsonl entry
    python3 task.py validate <dir>              # Validate jsonl files
    python3 task.py validate-lifecycle <dir> <activation|completion> [--allow-legacy] [--json]
    python3 task.py skill-registry validate [--json]
    python3 task.py skill-registry lookup (--skill <id> | --intent <intent>) [--phase <phase>]
    python3 task.py list-context <dir>          # List jsonl entries
    python3 task.py start <dir> [--allow-legacy]  # Gate transition and set active task
    python3 task.py current [--source]          # Show active task
    python3 task.py finish                      # Clear active task
    python3 task.py set-branch <dir> <branch>   # Set git branch
    python3 task.py set-base-branch <dir> <branch>  # Set PR target branch
    python3 task.py set-scope <dir> <scope>     # Set scope for PR title
    python3 task.py archive <task-dir> [--allow-legacy]  # Gate and archive task
    python3 task.py list                        # List active tasks
    python3 task.py list-archive [month]        # List archived tasks
    python3 task.py add-subtask <parent-dir> <child-dir>     # Link child to parent
    python3 task.py remove-subtask <parent-dir> <child-dir>  # Unlink child from parent
"""

from __future__ import annotations

import argparse
import json
from pathlib import Path
import sys

from common.log import Colors, colored
from common.paths import (
    DIR_WORKFLOW,
    DIR_TASKS,
    FILE_TASK_JSON,
    get_repo_root,
    get_developer,
    get_tasks_dir,
    get_current_task,
)
from common.active_task import (
    clear_active_task,
    resolve_active_task,
    resolve_context_key,
    set_active_task,
)
from common.io import read_json, write_json
from common.task_utils import resolve_task_dir, run_task_hooks
from common.lifecycle_contract import GateResult, validate_lifecycle_gate
from common.skill_registry import (
    RegistryError,
    load_registry,
    lookup_routes,
    lookup_skill,
    registry_path,
)
from common.tasks import iter_active_tasks, children_progress

# Import command handlers from split modules (also re-exports for plan.py compatibility)
from common.task_store import (
    cmd_create,
    cmd_archive,
    cmd_set_branch,
    cmd_set_base_branch,
    cmd_set_scope,
    cmd_add_subtask,
    cmd_remove_subtask,
)
from common.task_context import (
    cmd_add_context,
    cmd_validate,
    cmd_list_context,
)


def _print_gate_result(result: GateResult, as_json: bool = False) -> None:
    """Print a lifecycle validation result for humans or machine consumers."""
    if as_json:
        print(json.dumps(result.to_dict(), ensure_ascii=False, indent=2))
        return
    label = "passed" if result.ok else "blocked"
    color = Colors.GREEN if result.ok else Colors.RED
    print(colored(f"Lifecycle {result.gate} gate: {label}", color))
    for warning in result.warnings:
        print(colored(f"WARNING [{warning.code}] {warning.message}", Colors.YELLOW))
        if warning.hint:
            print(f"  Hint: {warning.hint}")
    for blocker in result.blockers:
        print(colored(f"BLOCKER [{blocker.code}] {blocker.message}", Colors.RED))
        if blocker.path:
            print(f"  Path: {blocker.path}")
        if blocker.hint:
            print(f"  Hint: {blocker.hint}")


def cmd_validate_lifecycle(args: argparse.Namespace) -> int:
    """Dry-run a lifecycle gate without mutating task or session state."""
    repo_root = get_repo_root()
    task_dir = resolve_task_dir(args.dir, repo_root)
    result = validate_lifecycle_gate(
        task_dir,
        repo_root,
        args.gate,
        allow_legacy=getattr(args, "allow_legacy", False),
    )
    _print_gate_result(result, getattr(args, "json", False))
    return 0 if result.ok else 1


def _validate_transition(
    task_dir: Path,
    repo_root: Path,
    gate: str,
    allow_legacy: bool,
) -> GateResult:
    """Run and print a lifecycle gate before any transition side effect."""
    result = validate_lifecycle_gate(task_dir, repo_root, gate, allow_legacy)
    if not result.ok:
        _print_gate_result(result)
    elif result.warnings:
        _print_gate_result(result)
    return result




# =============================================================================
# Command: start / finish
# =============================================================================

def cmd_start(args: argparse.Namespace) -> int:
    """Set active task, gating only a real planning-to-in-progress transition."""
    repo_root = get_repo_root()
    task_input = args.dir

    if not task_input:
        print(colored("Error: task directory or name required", Colors.RED))
        return 1

    full_path = resolve_task_dir(task_input, repo_root)
    if not full_path.is_dir():
        print(colored(f"Error: Task not found: {task_input}", Colors.RED))
        print("Hint: Use task name (e.g., 'my-task') or full path (e.g., '.trellis/tasks/01-31-my-task')")
        return 1

    try:
        task_ref = full_path.relative_to(repo_root).as_posix()
    except ValueError:
        task_ref = str(full_path)

    task_json_path = full_path / FILE_TASK_JSON
    task_data = read_json(task_json_path)
    if not isinstance(task_data, dict):
        gate_result = _validate_transition(
            full_path,
            repo_root,
            "activation",
            getattr(args, "allow_legacy", False),
        )
        if not gate_result.ok:
            return 1
        task_data = {}

    transition_required = task_data.get("status") == "planning"
    if transition_required:
        gate_result = _validate_transition(
            full_path,
            repo_root,
            "activation",
            getattr(args, "allow_legacy", False),
        )
        if not gate_result.ok:
            return 1

    context_key = resolve_context_key()
    if not context_key:
        print(colored(
            "ℹ Session identity not available; active-task pointer not persisted "
            "this session (degraded mode). AI continues based on conversation context.",
            Colors.YELLOW,
        ))
        print(colored(
            "Hint: run inside an AI IDE/session that exposes session identity, "
            "or set TRELLIS_CONTEXT_ID before running task.py start.",
            Colors.YELLOW,
        ))

        if transition_required:
            transitioned = dict(task_data)
            transitioned["status"] = "in_progress"
            if not write_json(task_json_path, transitioned):
                print(colored("Error: Failed to persist task status; start was not applied", Colors.RED))
                return 1
            print(colored("✓ Status: planning → in_progress (degraded)", Colors.GREEN))
        run_task_hooks("after_start", task_json_path, repo_root)
        return 0

    original_data = dict(task_data)
    if transition_required:
        transitioned = dict(task_data)
        transitioned["status"] = "in_progress"
        if not write_json(task_json_path, transitioned):
            print(colored("Error: Failed to persist task status; session pointer was not changed", Colors.RED))
            return 1

    active = set_active_task(task_ref, repo_root)
    if not active:
        if transition_required and not write_json(task_json_path, original_data):
            print(colored(
                "Error: Failed to set current task and could not roll task status back to planning",
                Colors.RED,
            ))
        else:
            print(colored("Error: Failed to set current task", Colors.RED))
        return 1

    print(colored(f"✓ Current task set to: {task_ref}", Colors.GREEN))
    print(f"Source: {active.source}")
    if transition_required:
        print(colored("✓ Status: planning → in_progress", Colors.GREEN))

    print()
    print(colored("The hook will now inject context from this task's jsonl files.", Colors.BLUE))
    run_task_hooks("after_start", task_json_path, repo_root)
    return 0


def cmd_finish(args: argparse.Namespace) -> int:
    """Clear active task."""
    repo_root = get_repo_root()
    active = clear_active_task(repo_root)
    current = active.task_path

    if not current:
        print(colored("No current task set", Colors.YELLOW))
        return 0

    # Resolve task.json path before clearing
    task_json_path = repo_root / current / FILE_TASK_JSON

    print(colored(f"✓ Cleared current task (was: {current})", Colors.GREEN))
    print(f"Source: {active.source}")

    if task_json_path.is_file():
        run_task_hooks("after_finish", task_json_path, repo_root)
    return 0


def cmd_current(args: argparse.Namespace) -> int:
    """Show active task."""
    repo_root = get_repo_root()
    active = resolve_active_task(repo_root)

    if args.source:
        print(f"Current task: {active.task_path or '(none)'}")
        print(f"Source: {active.source}")
        if active.stale:
            print("State: stale")
        return 0 if active.task_path else 1

    if active.task_path:
        print(active.task_path)
        return 0

    return 1


# =============================================================================
# Command: list
# =============================================================================

def cmd_list(args: argparse.Namespace) -> int:
    """List active tasks."""
    repo_root = get_repo_root()
    tasks_dir = get_tasks_dir(repo_root)
    current_task = get_current_task(repo_root)
    developer = get_developer(repo_root)
    filter_mine = args.mine
    filter_status = args.status

    if filter_mine:
        if not developer:
            print(colored("Error: No developer set. Run init_developer.py first", Colors.RED), file=sys.stderr)
            return 1
        print(colored(f"My tasks (assignee: {developer}):", Colors.BLUE))
    else:
        print(colored("All active tasks:", Colors.BLUE))
    print()

    # Single pass: collect all tasks via shared iterator
    all_tasks = {t.dir_name: t for t in iter_active_tasks(tasks_dir)}
    all_statuses = {name: t.status for name, t in all_tasks.items()}

    # Display tasks hierarchically
    count = 0

    def _print_task(dir_name: str, indent: int = 0) -> None:
        nonlocal count
        t = all_tasks[dir_name]

        # Apply --mine filter
        if filter_mine and (t.assignee or "-") != developer:
            return

        # Apply --status filter
        if filter_status and t.status != filter_status:
            return

        relative_path = f"{DIR_WORKFLOW}/{DIR_TASKS}/{dir_name}"
        marker = ""
        if relative_path == current_task:
            marker = f" {colored('<- current', Colors.GREEN)}"

        # Children progress
        progress = children_progress(t.children, all_statuses)

        # Package tag
        pkg_tag = f" @{t.package}" if t.package else ""

        prefix = "  " * indent + "  - "

        if filter_mine:
            print(f"{prefix}{dir_name}/ ({t.status}){pkg_tag}{progress}{marker}")
        else:
            print(f"{prefix}{dir_name}/ ({t.status}){pkg_tag}{progress} [{colored(t.assignee or '-', Colors.CYAN)}]{marker}")
        count += 1

        # Print children indented
        for child_name in t.children:
            if child_name in all_tasks:
                _print_task(child_name, indent + 1)

    # Display only top-level tasks (those without a parent)
    for dir_name in sorted(all_tasks.keys()):
        if not all_tasks[dir_name].parent:
            _print_task(dir_name)

    if count == 0:
        if filter_mine:
            print("  (no tasks assigned to you)")
        else:
            print("  (no active tasks)")

    print()
    print(f"Total: {count} task(s)")
    return 0


# =============================================================================
# Command: list-archive
# =============================================================================

def cmd_list_archive(args: argparse.Namespace) -> int:
    """List archived tasks."""
    repo_root = get_repo_root()
    tasks_dir = get_tasks_dir(repo_root)
    archive_dir = tasks_dir / "archive"
    month = args.month

    print(colored("Archived tasks:", Colors.BLUE))
    print()

    if month:
        month_dir = archive_dir / month
        if month_dir.is_dir():
            print(f"[{month}]")
            for d in sorted(month_dir.iterdir()):
                if d.is_dir():
                    print(f"  - {d.name}/")
        else:
            print(f"  No archives for {month}")
    else:
        if archive_dir.is_dir():
            for month_dir in sorted(archive_dir.iterdir()):
                if month_dir.is_dir():
                    month_name = month_dir.name
                    count = sum(1 for d in month_dir.iterdir() if d.is_dir())
                    print(f"[{month_name}] - {count} task(s)")

    return 0


def cmd_skill_registry(args: argparse.Namespace) -> int:
    """Validate or query the canonical skill registry."""
    repo_root = get_repo_root()
    try:
        registry = load_registry(registry_path(repo_root))
        if args.registry_command == "validate":
            result = {
                "schema": registry["schema"],
                "version": registry["version"],
                "valid": True,
                "skill_count": len(registry["skills"]),
            }
            if args.json:
                print(json.dumps(result, ensure_ascii=False, indent=2))
            else:
                print(colored(
                    f"Skill registry valid: {result['skill_count']} skills",
                    Colors.GREEN,
                ))
            return 0
        if args.registry_command == "lookup":
            if args.skill:
                payload: object = lookup_skill(registry, args.skill)
            else:
                payload = lookup_routes(registry, args.intent, args.phase)
            print(json.dumps(payload, ensure_ascii=False, indent=2))
            return 0
    except RegistryError as exc:
        print(colored(f"Error: {exc}", Colors.RED), file=sys.stderr)
        return 1
    print(colored("Error: skill-registry subcommand required", Colors.RED), file=sys.stderr)
    return 2


# =============================================================================
# Help
# =============================================================================

def show_usage() -> None:
    """Show usage help."""
    print("""Task Management Script

Usage:
  python3 task.py create <title>                     Create new task directory
  python3 task.py create <title> --package <pkg>     Create task for a specific package
  python3 task.py create <title> --parent <dir>      Create task as child of parent
  python3 task.py create <title> --no-start          Create without making it active in this session
  python3 task.py create <title> --complex           Require design and implementation plan
  python3 task.py add-context <dir> <jsonl> <path> [reason]  Add entry to jsonl
  python3 task.py validate <dir>                     Validate jsonl files
  python3 task.py validate-lifecycle <dir> <gate>    Dry-run activation/completion gate
  python3 task.py skill-registry validate [--json]    Validate skill contracts
  python3 task.py skill-registry lookup --skill <id> Lookup one skill contract
  python3 task.py skill-registry lookup --intent <i> [--phase <p>]  List deterministic routes
  python3 task.py list-context <dir>                 List jsonl entries
  python3 task.py start <dir> [--allow-legacy]       Validate and set active task
  python3 task.py current [--source]                 Show active task
  python3 task.py finish                             Clear active task
  python3 task.py set-branch <dir> <branch>          Set git branch
  python3 task.py set-base-branch <dir> <branch>     Set PR target branch
  python3 task.py set-scope <dir> <scope>            Set scope for PR title
  python3 task.py archive <task-dir> [--allow-legacy]  Validate and archive completed task
  python3 task.py add-subtask <parent> <child>       Link child task to parent
  python3 task.py remove-subtask <parent> <child>    Unlink child from parent
  python3 task.py list [--mine] [--status <status>]  List tasks
  python3 task.py list-archive [YYYY-MM]             List archived tasks

Monorepo options:
  --package <pkg>      Package name (validated against config.yaml packages)

List options:
  --mine, -m           Show only tasks assigned to current developer
  --status, -s <s>     Filter by status (planning, in_progress, review, completed)

Examples:
  python3 task.py create "Add login feature" --slug add-login
  python3 task.py create "Add login feature" --slug add-login --package cli
  python3 task.py create "Child task" --slug child --parent .trellis/tasks/01-21-parent
  python3 task.py add-context <dir> implement .trellis/spec/cli/backend/auth.md "Auth guidelines"
  python3 task.py set-branch <dir> task/add-login
  python3 task.py start .trellis/tasks/01-21-add-login
  python3 task.py current --source
  python3 task.py finish
  python3 task.py archive add-login
  python3 task.py add-subtask parent-task child-task  # Link existing tasks
  python3 task.py remove-subtask parent-task child-task
  python3 task.py list                               # List all active tasks
  python3 task.py list --mine                        # List my tasks only
  python3 task.py list --mine --status in_progress   # List my in-progress tasks
""")


# =============================================================================
# Main Entry
# =============================================================================

def main() -> int:
    """CLI entry point."""
    # Deprecation guard: `init-context` was removed in v0.5.0-beta.12.
    # Detect early so argparse doesn't mask the real reason with a generic
    # "invalid choice" error.
    if len(sys.argv) >= 2 and sys.argv[1] == "init-context":
        print(
            colored(
                "Error: `task.py init-context` was removed in v0.5.0-beta.12.",
                Colors.RED,
            ),
            file=sys.stderr,
        )
        print(
            "implement.jsonl / check.jsonl are now seeded on `task.py create` for",
            file=sys.stderr,
        )
        print(
            "sub-agent-capable platforms and curated by the AI during planning when needed.",
            file=sys.stderr,
        )
        print("See .trellis/workflow.md planning artifact guidance or run:", file=sys.stderr)
        print(
            "  python3 ./.trellis/scripts/get_context.py --mode phase --step 1",
            file=sys.stderr,
        )
        print(
            "Use `task.py add-context <dir> implement|check <path> <reason>` to append entries.",
            file=sys.stderr,
        )
        return 2

    parser = argparse.ArgumentParser(
        description="Task Management Script",
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    subparsers = parser.add_subparsers(dest="command", help="Commands")

    # create
    p_create = subparsers.add_parser("create", help="Create new task")
    p_create.add_argument("title", help="Task title")
    p_create.add_argument("--slug", "-s", help="Task slug without the MM-DD date prefix")
    p_create.add_argument("--assignee", "-a", help="Assignee developer")
    p_create.add_argument("--priority", "-p", default="P2", help="Priority (P0-P3)")
    p_create.add_argument("--description", "-d", help="Task description")
    p_create.add_argument("--parent", help="Parent task directory (establishes subtask link)")
    p_create.add_argument("--package", help="Package name for monorepo projects")
    p_create.add_argument(
        "--no-start",
        action="store_true",
        help="Create the task without making it active in this session",
    )
    p_create.add_argument("--complex", action="store_true", help="Require design.md and implement.md before activation")

    # add-context
    p_add = subparsers.add_parser("add-context", help="Add context entry")
    p_add.add_argument("dir", help="Task directory")
    p_add.add_argument("file", help="JSONL file (implement|check)")
    p_add.add_argument("path", help="File path to add")
    p_add.add_argument("reason", nargs="?", help="Reason for adding")

    # validate
    p_validate = subparsers.add_parser("validate", help="Validate context files")
    p_validate.add_argument("dir", help="Task directory")

    # validate-lifecycle (dry run)
    p_lifecycle = subparsers.add_parser("validate-lifecycle", help="Dry-run lifecycle gate")
    p_lifecycle.add_argument("dir", help="Task directory")
    p_lifecycle.add_argument("gate", choices=("activation", "completion"), help="Gate to validate")
    p_lifecycle.add_argument("--allow-legacy", action="store_true", help="Explicitly continue a legacy unversioned task")
    p_lifecycle.add_argument("--json", action="store_true", help="Print structured JSON result")

    # skill-registry
    p_registry = subparsers.add_parser("skill-registry", help="Validate or query skill routes")
    registry_subparsers = p_registry.add_subparsers(dest="registry_command")
    p_registry_validate = registry_subparsers.add_parser("validate", help="Validate bundled registry")
    p_registry_validate.add_argument("--json", action="store_true", help="Print structured JSON result")
    p_registry_lookup = registry_subparsers.add_parser("lookup", help="Lookup by skill or intent")
    registry_lookup_group = p_registry_lookup.add_mutually_exclusive_group(required=True)
    registry_lookup_group.add_argument("--skill", help="Exact skill id")
    registry_lookup_group.add_argument("--intent", help="Intent category")
    p_registry_lookup.add_argument("--phase", choices=("planning", "in_progress", "finish"))

    # list-context
    p_listctx = subparsers.add_parser("list-context", help="List context entries")
    p_listctx.add_argument("dir", help="Task directory")

    # start
    p_start = subparsers.add_parser("start", help="Set active task")
    p_start.add_argument("dir", help="Task directory")
    p_start.add_argument("--allow-legacy", action="store_true", help="Explicitly continue a legacy unversioned task")

    # current
    p_current = subparsers.add_parser("current", help="Show active task")
    p_current.add_argument("--source", action="store_true",
                           help="Show active task source")

    # finish
    subparsers.add_parser("finish", help="Clear active task")

    # set-branch
    p_branch = subparsers.add_parser("set-branch", help="Set git branch")
    p_branch.add_argument("dir", help="Task directory")
    p_branch.add_argument("branch", help="Branch name")

    # set-base-branch
    p_base = subparsers.add_parser("set-base-branch", help="Set PR target branch")
    p_base.add_argument("dir", help="Task directory")
    p_base.add_argument("base_branch", help="Base branch name (PR target)")

    # set-scope
    p_scope = subparsers.add_parser("set-scope", help="Set scope")
    p_scope.add_argument("dir", help="Task directory")
    p_scope.add_argument("scope", help="Scope name")

    # archive
    p_archive = subparsers.add_parser("archive", help="Archive task")
    p_archive.add_argument("name", help="Task directory or name")
    p_archive.add_argument("--no-commit", action="store_true", help="Skip auto git commit after archive")
    p_archive.add_argument("--allow-legacy", action="store_true", help="Explicitly archive a legacy unversioned task")

    # list
    p_list = subparsers.add_parser("list", help="List tasks")
    p_list.add_argument("--mine", "-m", action="store_true", help="My tasks only")
    p_list.add_argument("--status", "-s", help="Filter by status")

    # add-subtask
    p_addsub = subparsers.add_parser("add-subtask", help="Link child task to parent")
    p_addsub.add_argument("parent_dir", help="Parent task directory")
    p_addsub.add_argument("child_dir", help="Child task directory")

    # remove-subtask
    p_rmsub = subparsers.add_parser("remove-subtask", help="Unlink child task from parent")
    p_rmsub.add_argument("parent_dir", help="Parent task directory")
    p_rmsub.add_argument("child_dir", help="Child task directory")

    # list-archive
    p_listarch = subparsers.add_parser("list-archive", help="List archived tasks")
    p_listarch.add_argument("month", nargs="?", help="Month (YYYY-MM)")

    args = parser.parse_args()

    if not args.command:
        show_usage()
        return 1

    commands = {
        "create": cmd_create,
        "add-context": cmd_add_context,
        "validate": cmd_validate,
        "validate-lifecycle": cmd_validate_lifecycle,
        "skill-registry": cmd_skill_registry,
        "list-context": cmd_list_context,
        "start": cmd_start,
        "current": cmd_current,
        "finish": cmd_finish,
        "set-branch": cmd_set_branch,
        "set-base-branch": cmd_set_base_branch,
        "set-scope": cmd_set_scope,
        "archive": cmd_archive,
        "add-subtask": cmd_add_subtask,
        "remove-subtask": cmd_remove_subtask,
        "list": cmd_list,
        "list-archive": cmd_list_archive,
    }

    if args.command in commands:
        return commands[args.command](args)
    else:
        show_usage()
        return 1


if __name__ == "__main__":
    sys.exit(main())
