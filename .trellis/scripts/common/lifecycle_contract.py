"""Versioned lifecycle contracts and machine-enforced task gates.

The module is intentionally independent of session routing and lifecycle hooks.
It validates task-local artifacts and returns structured results; callers decide
whether to mutate lifecycle state only after ``ok`` is true.
"""

from __future__ import annotations

import json
from dataclasses import dataclass, field
from pathlib import Path, PureWindowsPath
from typing import Any, TypedDict

from .io import read_json

LIFECYCLE_CONTRACT_VERSION = 1
SCHEMA_VERSION = 1

REGISTRY_SCHEMA_ID = "trellis.skill-registry"
INVOCATION_SCHEMA_ID = "trellis.skill-invocation"
RESULT_SCHEMA_ID = "trellis.skill-result"
GATE_ERROR_SCHEMA_ID = "trellis.lifecycle-gate-error"
GATE_RESULT_SCHEMA_ID = "trellis.lifecycle-gate-result"
CHECK_EVIDENCE_SCHEMA_ID = "trellis.check-evidence"
REVIEW_EVIDENCE_SCHEMA_ID = "trellis.review-evidence"
SPEC_DISPOSITION_SCHEMA_ID = "trellis.spec-disposition"
COMMIT_READINESS_SCHEMA_ID = "trellis.commit-readiness"


class SkillRegistryEntry(TypedDict):
    """Stable registry entry consumed by the routing child task."""

    id: str
    version: int
    intents: list[str]
    phases: list[str]
    invocation_class: str
    required_inputs: list[str]
    optional_inputs: list[str]
    artifact_targets: list[str]
    allowed_side_effects: list[str]
    return_points: dict[str, str]
    recursive_dispatch: bool
    isolation_required: bool
    implementation_writes: bool
    external_projection: bool


class SkillRegistry(TypedDict):
    """Versioned registry envelope populated by the routing child task."""

    schema: str
    version: int
    skills: dict[str, SkillRegistryEntry]


class SkillInvocation(TypedDict):
    """Stable invocation envelope consumed by skill adapters."""

    schema: str
    version: int
    invocation_id: str
    skill_id: str
    task_path: str
    task_id: str
    contract_version: int
    phase: str
    intent: str
    source_refs: list[str]
    artifact_target: str
    allowed_side_effects: list[str]
    return_to: str
    platform_mode: str


class SkillResult(TypedDict):
    """Stable result envelope returned by skill adapters."""

    schema: str
    version: int
    invocation_id: str
    outcome: str
    artifacts_written: list[str]
    external_actions_requested: list[dict[str, Any]]
    checks_executed: list[dict[str, Any]]
    lifecycle_recommendation: str
    blockers: list[dict[str, str]]
    knowledge_candidates: list[dict[str, str]]


@dataclass(frozen=True)
class GateMessage:
    """Actionable machine-readable blocker or warning."""

    code: str
    message: str
    path: str | None = None
    hint: str | None = None

    def to_dict(self) -> dict[str, Any]:
        return {
            "schema": GATE_ERROR_SCHEMA_ID,
            "version": SCHEMA_VERSION,
            "code": self.code,
            "message": self.message,
            "path": self.path,
            "hint": self.hint,
        }


@dataclass
class GateResult:
    """Structured result shared by dry-run and mutation boundaries."""

    gate: str
    task_path: str
    contract_version: int | None
    legacy: bool = False
    blockers: list[GateMessage] = field(default_factory=list)
    warnings: list[GateMessage] = field(default_factory=list)

    @property
    def ok(self) -> bool:
        return not self.blockers

    def to_dict(self) -> dict[str, Any]:
        return {
            "schema": GATE_RESULT_SCHEMA_ID,
            "version": SCHEMA_VERSION,
            "ok": self.ok,
            "gate": self.gate,
            "task_path": self.task_path,
            "contract_version": self.contract_version,
            "legacy": self.legacy,
            "blockers": [item.to_dict() for item in self.blockers],
            "warnings": [item.to_dict() for item in self.warnings],
        }


def schema_catalog() -> dict[str, dict[str, Any]]:
    """Return stable schema identifiers and required fields for adapters."""
    return {
        REGISTRY_SCHEMA_ID: {
            "version": SCHEMA_VERSION,
            "required": ["schema", "version", "skills"],
            "entry_required": [
                "id", "version", "intents", "phases", "invocation_class",
                "required_inputs", "optional_inputs", "artifact_targets",
                "allowed_side_effects", "return_points", "recursive_dispatch",
                "isolation_required", "implementation_writes", "external_projection",
            ],
        },
        INVOCATION_SCHEMA_ID: {
            "version": SCHEMA_VERSION,
            "required": [
                "schema", "version", "invocation_id", "skill_id", "task_path",
                "task_id", "contract_version", "phase", "intent", "source_refs",
                "artifact_target", "allowed_side_effects", "return_to", "platform_mode",
            ],
        },
        RESULT_SCHEMA_ID: {
            "version": SCHEMA_VERSION,
            "required": [
                "schema", "version", "invocation_id", "outcome",
                "artifacts_written", "external_actions_requested", "checks_executed",
                "lifecycle_recommendation", "blockers", "knowledge_candidates",
            ],
        },
        GATE_ERROR_SCHEMA_ID: {
            "version": SCHEMA_VERSION,
            "required": ["schema", "version", "code", "message"],
            "optional": ["path", "hint"],
        },
        GATE_RESULT_SCHEMA_ID: {
            "version": SCHEMA_VERSION,
            "required": [
                "schema", "version", "ok", "gate", "task_path",
                "contract_version", "legacy", "blockers", "warnings",
            ],
        },
        CHECK_EVIDENCE_SCHEMA_ID: {
            "version": SCHEMA_VERSION,
            "required": ["schema", "version", "status", "checked_at", "scope", "commands"],
        },
        REVIEW_EVIDENCE_SCHEMA_ID: {
            "version": SCHEMA_VERSION,
            "required": ["schema", "version", "status", "reviewed_at", "axes"],
        },
        SPEC_DISPOSITION_SCHEMA_ID: {
            "version": SCHEMA_VERSION,
            "required": ["schema", "version", "status", "recorded_at", "rationale", "artifacts"],
            "optional": ["blocker"],
        },
        COMMIT_READINESS_SCHEMA_ID: {
            "version": SCHEMA_VERSION,
            "required": ["schema", "version", "status", "checked_at", "revision", "working_tree"],
        },
    }


def lifecycle_metadata(
    complexity: str,
    context_manifests_required: bool,
    review_required: bool = True,
) -> dict[str, Any]:
    """Build strict lifecycle metadata for newly created tasks."""
    return {
        "contract_version": LIFECYCLE_CONTRACT_VERSION,
        "complexity": complexity,
        "context_manifests_required": context_manifests_required,
        "review_required": review_required,
    }


def validate_lifecycle_gate(
    task_dir: Path,
    repo_root: Path,
    gate: str,
    allow_legacy: bool = False,
) -> GateResult:
    """Validate ``activation`` or ``completion`` without mutating the task."""
    result = GateResult(gate, _display_path(task_dir, repo_root), None)
    if gate not in ("activation", "completion"):
        result.blockers.append(GateMessage(
            "GATE_UNKNOWN",
            f"Unknown lifecycle gate: {gate}",
            hint="Use activation or completion.",
        ))
        return result

    task_json_path = task_dir / "task.json"
    task_data = read_json(task_json_path)
    if not isinstance(task_data, dict):
        result.blockers.append(GateMessage(
            "TASK_JSON_INVALID",
            "task.json is missing, invalid JSON, or not a JSON object.",
            _display_path(task_json_path, repo_root),
            "Restore a valid task.json object before changing lifecycle state.",
        ))
        return result

    meta = task_data.get("meta")
    raw_lifecycle = meta.get("lifecycle") if isinstance(meta, dict) else None
    if raw_lifecycle is not None and not isinstance(raw_lifecycle, dict):
        result.blockers.append(GateMessage(
            "LIFECYCLE_METADATA_INVALID",
            "task.json meta.lifecycle must be a JSON object.",
            _display_path(task_json_path, repo_root),
            "Replace meta.lifecycle with a valid versioned lifecycle object.",
        ))
        return result

    lifecycle = raw_lifecycle if isinstance(raw_lifecycle, dict) else {}
    version = lifecycle.get("contract_version") if lifecycle else None
    if type(version) is int:
        result.contract_version = version
    if version is None:
        result.legacy = True
        warning = GateMessage(
            "LEGACY_CONTRACT",
            "Task has no lifecycle contract version and cannot be strictly validated.",
            _display_path(task_json_path, repo_root),
            "Migrate task.json meta.lifecycle to contract_version 1, or rerun with --allow-legacy.",
        )
        result.warnings.append(warning)
        if not allow_legacy:
            result.blockers.append(GateMessage(
                "LEGACY_OVERRIDE_REQUIRED",
                "Legacy compatibility requires an explicit override.",
                _display_path(task_json_path, repo_root),
                "Review the task manually, then rerun with --allow-legacy.",
            ))
        return result

    if type(version) is not int or version != LIFECYCLE_CONTRACT_VERSION:
        result.blockers.append(GateMessage(
            "CONTRACT_VERSION_UNSUPPORTED",
            f"Unsupported lifecycle contract version: {version!r}.",
            _display_path(task_json_path, repo_root),
            f"Migrate to lifecycle contract version {LIFECYCLE_CONTRACT_VERSION}.",
        ))
        return result

    _validate_lifecycle_metadata(lifecycle, task_json_path, repo_root, result)
    if gate == "activation":
        _validate_activation(task_dir, repo_root, task_data, lifecycle, result)
    else:
        _validate_completion(task_dir, repo_root, task_data, lifecycle, result)
    return result


def _validate_lifecycle_metadata(
    lifecycle: dict[str, Any],
    task_json_path: Path,
    repo_root: Path,
    result: GateResult,
) -> None:
    display = _display_path(task_json_path, repo_root)
    if lifecycle.get("complexity") not in ("lightweight", "complex"):
        result.blockers.append(GateMessage(
            "COMPLEXITY_INVALID",
            "meta.lifecycle.complexity must be lightweight or complex.",
            display,
        ))
    for field_name in ("context_manifests_required", "review_required"):
        if type(lifecycle.get(field_name)) is not bool:
            result.blockers.append(GateMessage(
                "LIFECYCLE_METADATA_INVALID",
                f"meta.lifecycle.{field_name} must be true or false.",
                display,
            ))


def _validate_activation(
    task_dir: Path,
    repo_root: Path,
    task_data: dict[str, Any],
    lifecycle: dict[str, Any],
    result: GateResult,
) -> None:
    if task_data.get("status") != "planning":
        result.blockers.append(GateMessage(
            "ACTIVATION_STATUS_INVALID",
            "Strict activation requires task status planning.",
            _display_path(task_dir / "task.json", repo_root),
            "Activate only a planning task.",
        ))

    _validate_prd(task_dir / "prd.md", repo_root, result)
    if lifecycle.get("complexity") == "complex":
        _require_nonempty_file(task_dir / "design.md", repo_root, "DESIGN_REQUIRED", result)
        _require_nonempty_file(task_dir / "implement.md", repo_root, "IMPLEMENT_PLAN_REQUIRED", result)

    if lifecycle.get("context_manifests_required") is True:
        for name in ("implement.jsonl", "check.jsonl"):
            _validate_manifest(task_dir / name, repo_root, result)


def _validate_completion(
    task_dir: Path,
    repo_root: Path,
    task_data: dict[str, Any],
    lifecycle: dict[str, Any],
    result: GateResult,
) -> None:
    if task_data.get("status") != "in_progress":
        result.blockers.append(GateMessage(
            "COMPLETION_STATUS_INVALID",
            "Strict completion requires task status in_progress.",
            _display_path(task_dir / "task.json", repo_root),
            "Complete implementation before archiving.",
        ))

    evidence_dir = task_dir / "evidence"
    _validate_check_evidence(evidence_dir / "check.json", repo_root, result)
    if lifecycle.get("review_required") is True:
        _validate_review_evidence(evidence_dir / "review.json", repo_root, result)
    _validate_spec_disposition(evidence_dir / "spec-disposition.json", repo_root, result)
    _validate_commit_readiness(evidence_dir / "commit-readiness.json", repo_root, result)


def _validate_prd(path: Path, repo_root: Path, result: GateResult) -> None:
    try:
        content = path.read_text(encoding="utf-8") if path.is_file() else ""
    except OSError:
        content = ""
    if not content.strip():
        _require_nonempty_file(path, repo_root, "PRD_REQUIRED", result)
        return
    seed_markers = {"TBD.", "- TBD", "- [ ] TBD"}
    if any(line.strip() in seed_markers for line in content.splitlines()):
        result.blockers.append(GateMessage(
            "PRD_NOT_READY",
            "prd.md still contains task-creation seed placeholders.",
            _display_path(path, repo_root),
            "Replace the TBD goal, requirements, and acceptance criteria before activation.",
        ))


def _require_nonempty_file(path: Path, repo_root: Path, code: str, result: GateResult) -> None:
    try:
        valid = path.is_file() and bool(path.read_text(encoding="utf-8").strip())
    except OSError:
        valid = False
    if not valid:
        result.blockers.append(GateMessage(
            code,
            f"Required planning artifact is missing or empty: {path.name}.",
            _display_path(path, repo_root),
            f"Create and review {path.name} before activation.",
        ))


def _validate_manifest(path: Path, repo_root: Path, result: GateResult) -> None:
    display = _display_path(path, repo_root)
    if not path.is_file():
        result.blockers.append(GateMessage(
            "CONTEXT_MANIFEST_MISSING", f"Required context manifest is missing: {path.name}.",
            display, "Create the manifest and add at least one real context entry.",
        ))
        return

    try:
        lines = path.read_text(encoding="utf-8").splitlines()
    except OSError:
        result.blockers.append(GateMessage(
            "CONTEXT_MANIFEST_UNREADABLE", f"Required context manifest cannot be read: {path.name}.",
            display,
        ))
        return

    real_entries = 0
    repo_resolved = repo_root.resolve()
    for line_number, line in enumerate(lines, 1):
        if not line.strip():
            continue
        try:
            entry = json.loads(line)
        except json.JSONDecodeError:
            result.blockers.append(GateMessage(
                "CONTEXT_MANIFEST_JSON_INVALID",
                f"{path.name}:{line_number} is not valid JSON.", display,
            ))
            continue
        if not isinstance(entry, dict):
            result.blockers.append(GateMessage(
                "CONTEXT_MANIFEST_ENTRY_INVALID",
                f"{path.name}:{line_number} must be a JSON object.", display,
            ))
            continue

        target = entry.get("file")
        if target is None and "_example" in entry:
            continue
        if not isinstance(target, str) or not target.strip():
            result.blockers.append(GateMessage(
                "CONTEXT_MANIFEST_ENTRY_INVALID",
                f"{path.name}:{line_number} requires a non-empty string file path.", display,
            ))
            continue

        real_entries += 1
        reason = entry.get("reason")
        if not isinstance(reason, str) or not reason.strip():
            result.blockers.append(GateMessage(
                "CONTEXT_MANIFEST_REASON_REQUIRED",
                f"{path.name}:{line_number} requires a non-empty reason.", display,
            ))

        entry_type = entry.get("type", "file")
        if entry_type not in ("file", "directory"):
            result.blockers.append(GateMessage(
                "CONTEXT_MANIFEST_TYPE_INVALID",
                f"{path.name}:{line_number} type must be file or directory.", display,
            ))
            continue

        logical_target = Path(target.replace("\\", "/"))
        if logical_target.is_absolute() or PureWindowsPath(target).is_absolute():
            result.blockers.append(GateMessage(
                "CONTEXT_TARGET_OUTSIDE_REPO",
                f"{path.name}:{line_number} target must be repository-relative: {target}.",
                display,
            ))
            continue
        try:
            target_path = (repo_root / logical_target).resolve()
            target_path.relative_to(repo_resolved)
        except (OSError, RuntimeError, ValueError):
            result.blockers.append(GateMessage(
                "CONTEXT_TARGET_OUTSIDE_REPO",
                f"{path.name}:{line_number} target must stay inside the repository: {target}.",
                display,
            ))
            continue

        exists = target_path.is_dir() if entry_type == "directory" else target_path.is_file()
        if not exists:
            result.blockers.append(GateMessage(
                "CONTEXT_TARGET_MISSING",
                f"{path.name}:{line_number} target does not exist: {target}.", display,
                "Fix or remove the stale context entry.",
            ))
    if real_entries == 0:
        result.blockers.append(GateMessage(
            "CONTEXT_MANIFEST_NOT_READY",
            f"{path.name} has no real context entries.", display,
            "Replace the _example seed with at least one validated file or directory entry.",
        ))


def _load_evidence(
    path: Path,
    repo_root: Path,
    schema_id: str,
    result: GateResult,
) -> dict[str, Any] | None:
    display = _display_path(path, repo_root)
    data = read_json(path)
    if not isinstance(data, dict):
        result.blockers.append(GateMessage(
            "EVIDENCE_MISSING_OR_INVALID",
            f"Required evidence is missing, invalid, or not a JSON object: {path.name}.",
            display, f"Write {path.name} using schema {schema_id} version {SCHEMA_VERSION}.",
        ))
        return None
    version = data.get("version")
    if data.get("schema") != schema_id or type(version) is not int or version != SCHEMA_VERSION:
        result.blockers.append(GateMessage(
            "EVIDENCE_SCHEMA_INVALID",
            f"{path.name} must use schema {schema_id} version {SCHEMA_VERSION}.", display,
        ))
        return None
    return data


def _require_fields(
    data: dict[str, Any],
    fields: list[str],
    path: Path,
    repo_root: Path,
    result: GateResult,
) -> bool:
    missing = [name for name in fields if data.get(name) in (None, "", [])]
    if not missing:
        return True
    result.blockers.append(GateMessage(
        "EVIDENCE_FIELDS_MISSING",
        f"{path.name} is missing required fields: {', '.join(missing)}.",
        _display_path(path, repo_root),
    ))
    return False


def _is_nonempty_string(value: Any) -> bool:
    return isinstance(value, str) and bool(value.strip())


def _is_string_list(value: Any) -> bool:
    return (
        isinstance(value, list)
        and bool(value)
        and all(_is_nonempty_string(item) for item in value)
    )

def _are_existing_repo_files(value: Any, repo_root: Path) -> bool:
    if not _is_string_list(value):
        return False
    repo_resolved = repo_root.resolve()
    for item in value:
        logical_path = Path(item.replace("\\", "/"))
        if logical_path.is_absolute() or PureWindowsPath(item).is_absolute():
            return False
        try:
            full_path = (repo_root / logical_path).resolve()
            full_path.relative_to(repo_resolved)
        except (OSError, RuntimeError, ValueError):
            return False
        if not full_path.is_file():
            return False
    return True


def _validate_check_evidence(path: Path, repo_root: Path, result: GateResult) -> None:
    data = _load_evidence(path, repo_root, CHECK_EVIDENCE_SCHEMA_ID, result)
    if data is None or not _require_fields(data, ["status", "checked_at", "scope", "commands"], path, repo_root, result):
        return
    commands = data.get("commands")
    commands_pass = isinstance(commands, list) and bool(commands) and all(
        isinstance(command, dict)
        and command.get("status") == "passed"
        and type(command.get("exit_code")) is int
        and command.get("exit_code") == 0
        and _is_nonempty_string(command.get("command"))
        for command in commands
    )
    valid = (
        data.get("status") == "passed"
        and _is_nonempty_string(data.get("checked_at"))
        and _is_string_list(data.get("scope"))
        and commands_pass
    )
    if not valid:
        result.blockers.append(GateMessage(
            "CHECK_NOT_PASSED",
            "Check evidence must record a non-empty scope and passed commands with exit_code 0.",
            _display_path(path, repo_root),
        ))


def _validate_review_evidence(path: Path, repo_root: Path, result: GateResult) -> None:
    data = _load_evidence(path, repo_root, REVIEW_EVIDENCE_SCHEMA_ID, result)
    if data is None or not _require_fields(data, ["status", "reviewed_at", "axes"], path, repo_root, result):
        return
    axes = data.get("axes")
    axes_pass = isinstance(axes, dict) and axes.get("standards") == "passed" and axes.get("spec") == "passed"
    if data.get("status") != "passed" or not _is_nonempty_string(data.get("reviewed_at")) or not axes_pass:
        result.blockers.append(GateMessage(
            "REVIEW_NOT_PASSED", "Review evidence must pass both standards and spec axes.",
            _display_path(path, repo_root),
        ))


def _validate_spec_disposition(path: Path, repo_root: Path, result: GateResult) -> None:
    data = _load_evidence(path, repo_root, SPEC_DISPOSITION_SCHEMA_ID, result)
    if data is None or not _require_fields(data, ["status", "recorded_at", "rationale"], path, repo_root, result):
        return
    status = data.get("status")
    artifacts = data.get("artifacts")
    valid_shape = (
        status in ("updated", "not_needed", "deferred")
        and _is_nonempty_string(data.get("recorded_at"))
        and _is_nonempty_string(data.get("rationale"))
        and (
            _are_existing_repo_files(artifacts, repo_root)
            if status == "updated"
            else artifacts == []
        )
    )
    if not valid_shape:
        result.blockers.append(GateMessage(
            "SPEC_DISPOSITION_INVALID",
            "Spec disposition must be updated, not_needed, or deferred with valid artifact paths.",
            _display_path(path, repo_root),
        ))
        return
    if status == "deferred":
        blocker = data.get("blocker")
        blocker_message = (
            blocker.strip()
            if isinstance(blocker, str) and blocker.strip()
            else "Spec update is deferred without an explicit blocker."
        )
        result.blockers.append(GateMessage(
            "SPEC_UPDATE_DEFERRED",
            blocker_message,
            _display_path(path, repo_root),
            "Resolve the spec blocker before archiving.",
        ))


def _validate_commit_readiness(path: Path, repo_root: Path, result: GateResult) -> None:
    data = _load_evidence(path, repo_root, COMMIT_READINESS_SCHEMA_ID, result)
    if data is None or not _require_fields(data, ["status", "checked_at", "revision", "working_tree"], path, repo_root, result):
        return
    valid = (
        data.get("status") == "ready"
        and data.get("working_tree") == "clean"
        and _is_nonempty_string(data.get("checked_at"))
        and _is_nonempty_string(data.get("revision"))
    )
    if not valid:
        result.blockers.append(GateMessage(
            "COMMIT_NOT_READY", "Commit readiness must record status ready and working_tree clean.",
            _display_path(path, repo_root),
        ))


def _display_path(path: Path, repo_root: Path) -> str:
    try:
        return path.relative_to(repo_root).as_posix()
    except ValueError:
        return str(path)
