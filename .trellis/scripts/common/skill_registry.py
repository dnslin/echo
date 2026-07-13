"""Skill routing, invocation, result, and artifact-boundary contracts.

The bundled ``.trellis/skill-registry.json`` is the sole skill routing data
source. This module consumes schema identifiers and typed envelopes from
``lifecycle_contract`` instead of defining a parallel schema catalog.
"""

from __future__ import annotations

import fnmatch
from pathlib import Path, PurePosixPath, PureWindowsPath
from typing import Any, Protocol, TypedDict

from .io import read_json
from .lifecycle_contract import (
    INVOCATION_SCHEMA_ID,
    LIFECYCLE_CONTRACT_VERSION,
    REGISTRY_SCHEMA_ID,
    RESULT_SCHEMA_ID,
    SCHEMA_VERSION,
    SkillInvocation,
    SkillRegistry,
    SkillRegistryEntry,
    SkillResult,
    schema_catalog,
)

REGISTRY_FILE = "skill-registry.json"
ALLOWED_PHASES = frozenset(("planning", "in_progress", "finish"))
ALLOWED_INVOCATION_CLASSES = frozenset((
    "advice",
    "task-artifact",
    "isolated-investigation",
    "implementation",
    "review",
    "release-governance",
))
ALLOWED_SIDE_EFFECTS = frozenset((
    "read-repo",
    "read-git",
    "network-research",
    "task-artifact-write",
    "product-code-write",
    "test-write",
    "isolated-workspace-write",
    "spawn-readonly-reviewers",
    "child-task-request",
    "tracker-projection-request",
    "global-knowledge-proposal",
    "global-spec-write",
    "release-artifact-write",
    "lifecycle-finish",
    "lifecycle-archive",
    "lifecycle-commit",
))
FORBIDDEN_SPECIALIST_SIDE_EFFECTS = frozenset((
    "task-status-write",
    "active-task-select",
    "recursive-lifecycle-dispatch",
    "tracker-canonical-write",
    "external-publish",
    "commit",
    "archive",
))
PHASE_ALLOWED_SIDE_EFFECTS = {
    "planning": frozenset((
        "read-repo",
        "read-git",
        "network-research",
        "task-artifact-write",
        "isolated-workspace-write",
        "spawn-readonly-reviewers",
        "child-task-request",
        "tracker-projection-request",
        "global-knowledge-proposal",
    )),
    "in_progress": frozenset((
        "read-repo",
        "read-git",
        "network-research",
        "task-artifact-write",
        "product-code-write",
        "test-write",
        "isolated-workspace-write",
        "spawn-readonly-reviewers",
        "child-task-request",
        "tracker-projection-request",
        "global-knowledge-proposal",
        "release-artifact-write",
    )),
    "finish": frozenset((
        "read-repo",
        "read-git",
        "network-research",
        "task-artifact-write",
        "spawn-readonly-reviewers",
        "tracker-projection-request",
        "global-knowledge-proposal",
        "global-spec-write",
        "release-artifact-write",
        "lifecycle-finish",
        "lifecycle-archive",
        "lifecycle-commit",
    )),
}
LIFECYCLE_OWNER = "trellis-finish-work"
GLOBAL_TARGET_PREFIXES = ("@repo/.trellis/spec/",)


class RegistryError(ValueError):
    """Raised when registry or envelope data violates its contract."""


class TrackerProjectionRequest(TypedDict):
    """Provider-neutral request for a future tracker projection adapter."""

    task_path: str
    projection_kind: str
    tracker_config: dict[str, Any]


class TrackerProjectionResult(TypedDict):
    """Provider-neutral projection response."""

    external_refs: list[str]
    drift_detected: bool
    warnings: list[str]


class TrackerProjection(Protocol):
    """Interface contract only; Trellis intentionally bundles no provider."""

    def project_task(
        self,
        request: TrackerProjectionRequest,
    ) -> TrackerProjectionResult:
        """Project canonical task artifacts without changing Trellis state."""
        ...


def registry_path(repo_root: Path) -> Path:
    """Return the canonical bundled registry location in an installed repo."""
    return repo_root / ".trellis" / REGISTRY_FILE


def load_registry(path: Path) -> SkillRegistry:
    """Load and validate a registry JSON document."""
    data = read_json(path)
    errors = validate_registry(data)
    if errors:
        raise RegistryError("; ".join(errors))
    return data  # type: ignore[return-value]


def validate_registry(data: Any) -> list[str]:
    """Return deterministic validation errors for registry data."""
    errors: list[str] = []
    catalog = schema_catalog()[REGISTRY_SCHEMA_ID]
    if not isinstance(data, dict):
        return ["registry must be a JSON object"]
    _require_keys(data, catalog["required"], "registry", errors)
    if data.get("schema") != REGISTRY_SCHEMA_ID:
        errors.append(f"registry.schema must be {REGISTRY_SCHEMA_ID!r}")
    if data.get("version") != catalog["version"]:
        errors.append(f"registry.version must be {catalog['version']}")
    skills = data.get("skills")
    if not isinstance(skills, dict) or not skills:
        errors.append("registry.skills must be a non-empty object")
        return errors
    for skill_id in sorted(skills):
        entry = skills[skill_id]
        label = f"skills.{skill_id}"
        if not isinstance(entry, dict):
            errors.append(f"{label} must be an object")
            continue
        _require_keys(entry, catalog["entry_required"], label, errors)
        if entry.get("id") != skill_id:
            errors.append(f"{label}.id must equal its registry key")
        if entry.get("version") != SCHEMA_VERSION:
            errors.append(f"{label}.version must be {SCHEMA_VERSION}")
        _validate_string_list(entry, "intents", label, errors, nonempty=True)
        _validate_string_list(entry, "phases", label, errors, nonempty=True)
        _validate_string_list(entry, "required_inputs", label, errors)
        _validate_string_list(entry, "optional_inputs", label, errors)
        _validate_string_list(entry, "artifact_targets", label, errors)
        _validate_string_list(entry, "allowed_side_effects", label, errors)
        phases = entry.get("phases")
        if isinstance(phases, list):
            unknown = sorted(set(phases) - ALLOWED_PHASES)
            if unknown:
                errors.append(f"{label}.phases contains unsupported values: {unknown}")
        invocation_class = entry.get("invocation_class")
        if invocation_class not in ALLOWED_INVOCATION_CLASSES:
            errors.append(f"{label}.invocation_class is unsupported: {invocation_class!r}")
        effects = entry.get("allowed_side_effects")
        if isinstance(effects, list):
            unknown_effects = sorted(set(effects) - ALLOWED_SIDE_EFFECTS)
            forbidden = sorted(set(effects) & FORBIDDEN_SPECIALIST_SIDE_EFFECTS)
            if unknown_effects:
                errors.append(f"{label}.allowed_side_effects contains unsupported values: {unknown_effects}")
            if forbidden:
                errors.append(f"{label}.allowed_side_effects contains forbidden values: {forbidden}")
            lifecycle_effects = sorted(effect for effect in effects if effect.startswith("lifecycle-"))
            if lifecycle_effects and skill_id != LIFECYCLE_OWNER:
                errors.append(f"{label} cannot own lifecycle side effects: {lifecycle_effects}")
        for key in ("recursive_dispatch", "isolation_required", "implementation_writes", "external_projection"):
            if type(entry.get(key)) is not bool:
                errors.append(f"{label}.{key} must be boolean")
        if isinstance(effects, list):
            projection_effect = "tracker-projection-request" in effects
            if entry.get("external_projection") is not projection_effect:
                errors.append(
                    f"{label}.external_projection must match tracker-projection-request permission"
                )
        if entry.get("recursive_dispatch") is not False:
            errors.append(f"{label}.recursive_dispatch must be false")
        return_points = entry.get("return_points")
        if not isinstance(return_points, dict) or not return_points:
            errors.append(f"{label}.return_points must be a non-empty object")
        elif any(not isinstance(key, str) or not isinstance(value, str) or not value for key, value in return_points.items()):
            errors.append(f"{label}.return_points must map non-empty strings to non-empty strings")
        targets = entry.get("artifact_targets")
        if isinstance(targets, list):
            for target in targets:
                if isinstance(target, str):
                    target_error = _validate_target_pattern(target, effects if isinstance(effects, list) else [])
                    if target_error:
                        errors.append(f"{label}.artifact_targets: {target_error}")
    return errors



def lookup_skill(registry: SkillRegistry, skill_id: str) -> SkillRegistryEntry:
    """Return one skill contract or raise an actionable lookup error."""
    entry = registry["skills"].get(skill_id)
    if entry is None:
        raise RegistryError(f"unknown skill: {skill_id}")
    return entry


def lookup_routes(
    registry: SkillRegistry,
    intent: str,
    phase: str | None = None,
) -> list[SkillRegistryEntry]:
    """Return matching routes sorted by skill id for deterministic routing."""
    if phase is not None and phase not in ALLOWED_PHASES:
        raise RegistryError(f"unsupported phase: {phase}")
    matches = [
        entry
        for entry in registry["skills"].values()
        if intent in entry["intents"] and (phase is None or phase in entry["phases"])
    ]
    return sorted(matches, key=lambda item: item["id"])


def construct_invocation(
    registry: SkillRegistry,
    repo_root: Path,
    task_dir: Path,
    skill_id: str,
    intent: str,
    phase: str,
    artifact_target: str,
    invocation_id: str,
    source_refs: list[str] | None = None,
    allowed_side_effects: list[str] | None = None,
    return_to: str | None = None,
    platform_mode: str = "inline",
) -> SkillInvocation:
    """Build and validate an invocation envelope from canonical task data."""
    entry = lookup_skill(registry, skill_id)
    task_data = read_json(task_dir / "task.json")
    if not isinstance(task_data, dict) or not isinstance(task_data.get("id"), str):
        raise RegistryError("task.json must contain a string id")
    selected_return = return_to
    if selected_return is None:
        selected_return = _default_return_point(entry, phase)
    invocation: SkillInvocation = {
        "schema": INVOCATION_SCHEMA_ID,
        "version": SCHEMA_VERSION,
        "invocation_id": invocation_id,
        "skill_id": skill_id,
        "task_path": _repo_relative(task_dir, repo_root),
        "task_id": task_data["id"],
        "contract_version": LIFECYCLE_CONTRACT_VERSION,
        "phase": phase,
        "intent": intent,
        "source_refs": list(source_refs or []),
        "artifact_target": artifact_target,
        "allowed_side_effects": list(
            allowed_side_effects
            if allowed_side_effects is not None
            else _effects_for_phase(entry["allowed_side_effects"], phase)
        ),
        "return_to": selected_return,
        "platform_mode": platform_mode,
    }
    validate_invocation(invocation, registry, repo_root)
    return invocation


def validate_invocation(
    invocation: Any,
    registry: SkillRegistry,
    repo_root: Path,
) -> SkillInvocation:
    """Validate an invocation against schema, route, phase, and write policy."""
    _validate_envelope(invocation, INVOCATION_SCHEMA_ID)
    for key in (
        "invocation_id", "skill_id", "task_path", "task_id", "phase",
        "intent", "artifact_target", "return_to", "platform_mode",
    ):
        if not isinstance(invocation.get(key), str):
            raise RegistryError(f"invocation {key} must be a string")
    source_refs = invocation.get("source_refs")
    if not isinstance(source_refs, list) or any(not isinstance(item, str) for item in source_refs):
        raise RegistryError("invocation source_refs must be a string list")
    entry = lookup_skill(registry, invocation["skill_id"])
    if invocation["contract_version"] != LIFECYCLE_CONTRACT_VERSION:
        raise RegistryError("invocation contract_version is unsupported")
    if invocation["intent"] not in entry["intents"]:
        raise RegistryError("invocation intent is not registered for the skill")
    if invocation["phase"] not in entry["phases"]:
        raise RegistryError("invocation phase is not registered for the skill")
    if invocation["return_to"] not in entry["return_points"].values():
        raise RegistryError("invocation return_to is not registered for the skill")
    requested_effects = invocation["allowed_side_effects"]
    if not isinstance(requested_effects, list) or any(not isinstance(item, str) for item in requested_effects):
        raise RegistryError("invocation allowed_side_effects must be a string list")
    if not set(requested_effects).issubset(entry["allowed_side_effects"]):
        raise RegistryError("invocation requests side effects outside the skill contract")
    phase_effects = PHASE_ALLOWED_SIDE_EFFECTS[invocation["phase"]]
    disallowed_for_phase = sorted(set(requested_effects) - phase_effects)
    if disallowed_for_phase:
        raise RegistryError(
            f"invocation requests side effects forbidden during {invocation['phase']}: "
            f"{disallowed_for_phase}"
        )
    task_dir = _resolve_task_path(repo_root, invocation["task_path"])
    task_data = read_json(task_dir / "task.json")
    if not isinstance(task_data, dict) or task_data.get("id") != invocation["task_id"]:
        raise RegistryError("invocation task_id does not match task.json")
    expected_status = "planning" if invocation["phase"] == "planning" else "in_progress"
    if task_data.get("status") != expected_status:
        raise RegistryError(
            f"invocation phase {invocation['phase']} requires task status {expected_status}"
        )
    _validate_required_inputs(entry, task_dir, source_refs)
    target = invocation["artifact_target"]
    if target:
        validate_artifact_target(task_dir, repo_root, target, entry, requested_effects)
    elif entry["artifact_targets"]:
        raise RegistryError("invocation artifact_target is required for this skill")
    return invocation  # type: ignore[return-value]

def validate_result(
    result: Any,
    invocation: SkillInvocation,
    registry: SkillRegistry,
    repo_root: Path,
) -> SkillResult:
    """Validate returned artifacts/actions and lifecycle recommendation."""
    _validate_envelope(result, RESULT_SCHEMA_ID)
    if result["invocation_id"] != invocation["invocation_id"]:
        raise RegistryError("result invocation_id does not match invocation")
    if not isinstance(result.get("outcome"), str) or not result["outcome"]:
        raise RegistryError("result outcome must be a non-empty string")
    for key in ("checks_executed", "blockers", "knowledge_candidates"):
        value = result.get(key)
        if not isinstance(value, list) or any(not isinstance(item, dict) for item in value):
            raise RegistryError(f"result {key} must be an object list")
    entry = lookup_skill(registry, invocation["skill_id"])
    recommendation = result["lifecycle_recommendation"]
    if recommendation not in entry["return_points"].values():
        raise RegistryError("result lifecycle_recommendation is not a registered return seam")
    blockers = result["blockers"]
    blocking_return = entry["return_points"].get("blocking")
    if blockers and blocking_return is not None and recommendation != blocking_return:
        raise RegistryError("result with blockers must use the registered blocking return seam")
    artifacts = result["artifacts_written"]
    if not isinstance(artifacts, list) or any(not isinstance(item, str) for item in artifacts):
        raise RegistryError("result artifacts_written must be a string list")
    if invocation["artifact_target"] and artifacts != [invocation["artifact_target"]]:
        raise RegistryError("result artifacts must contain exactly the invocation artifact_target")
    task_dir = _resolve_task_path(repo_root, invocation["task_path"])
    for artifact in artifacts:
        resolved = validate_artifact_target(
            task_dir,
            repo_root,
            artifact,
            entry,
            invocation["allowed_side_effects"],
        )
        if not resolved.is_file():
            raise RegistryError(f"result artifact does not exist: {artifact}")
    actions = result["external_actions_requested"]
    if not isinstance(actions, list) or any(not isinstance(item, dict) for item in actions):
        raise RegistryError("result external_actions_requested must be an object list")
    if actions and (not entry["external_projection"] or "tracker-projection-request" not in invocation["allowed_side_effects"]):
        raise RegistryError("result requests external actions without projection permission")
    for action in actions:
        if action.get("kind") != "tracker-projection":
            raise RegistryError("external actions must use the tracker-projection seam")
        if not isinstance(action.get("projection_kind"), str):
            raise RegistryError("tracker projection action requires projection_kind")
    if result["knowledge_candidates"] and "global-knowledge-proposal" not in invocation["allowed_side_effects"]:
        raise RegistryError("result returns knowledge candidates without proposal permission")
    return result  # type: ignore[return-value]


def _default_return_point(entry: SkillRegistryEntry, phase: str) -> str:
    """Choose the lifecycle seam that corresponds to the invocation phase."""
    phase_key = {
        "planning": "planning",
        "in_progress": "implementation",
        "finish": "finish",
    }.get(phase)
    if phase_key is not None:
        phase_return = entry["return_points"].get(phase_key)
        if phase_return is not None:
            return phase_return
    success_return = entry["return_points"].get("success")
    if success_return is not None:
        return success_return
    return sorted(entry["return_points"].values())[0]


def _validate_required_inputs(
    entry: SkillRegistryEntry,
    task_dir: Path,
    source_refs: list[str],
) -> None:
    """Require every declared invocation input before specialist execution."""
    for required_input in entry["required_inputs"]:
        if required_input == "task_path":
            continue
        if required_input == "source_refs":
            if not source_refs:
                raise RegistryError("invocation requires at least one source_ref")
            continue
        if any(token in required_input for token in ("*", "?", "[", "]")):
            raise RegistryError(f"required input must be a concrete task path: {required_input}")
        normalized = _normalize_logical_path(required_input)
        if normalized.startswith("@repo/"):
            raise RegistryError(f"required input must be task-local: {required_input}")
        input_path = (task_dir / normalized).resolve()
        _require_within(input_path, task_dir.resolve(), "required input escapes task directory")
        if not input_path.exists():
            raise RegistryError(f"required input does not exist: {required_input}")


def validate_artifact_target(
    task_dir: Path,
    repo_root: Path,
    target: str,
    entry: SkillRegistryEntry,
    effects: list[str],
) -> Path:
    """Resolve an artifact target and enforce task-local or approved global jail."""
    if any(token in target for token in ("*", "?", "[", "]")):
        raise RegistryError("artifact target must be a concrete path, not a pattern")
    normalized = _normalize_logical_path(target)
    if normalized.startswith("@repo/"):
        if "global-spec-write" not in effects:
            raise RegistryError("global artifact target requires global-spec-write")
        if not normalized.startswith(GLOBAL_TARGET_PREFIXES):
            raise RegistryError("global artifact target is outside approved global prefixes")
        relative = normalized[len("@repo/"):]
        resolved = (repo_root / relative).resolve()
        _require_within(resolved, repo_root.resolve(), "global artifact target escapes repository")
    else:
        resolved = (task_dir / normalized).resolve()
        _require_within(resolved, task_dir.resolve(), "artifact target escapes task directory")
    if not any(_target_matches(normalized, pattern) for pattern in entry["artifact_targets"]):
        raise RegistryError("artifact target is not permitted by the skill contract")
    return resolved


def _validate_envelope(data: Any, schema_id: str) -> None:
    if not isinstance(data, dict):
        raise RegistryError("envelope must be a JSON object")
    catalog = schema_catalog()[schema_id]
    missing = [key for key in catalog["required"] if key not in data]
    if missing:
        raise RegistryError(f"envelope missing required fields: {missing}")
    if data.get("schema") != schema_id or data.get("version") != catalog["version"]:
        raise RegistryError(f"envelope must use {schema_id} version {catalog['version']}")


def _require_keys(data: dict[str, Any], required: list[str], label: str, errors: list[str]) -> None:
    missing = [key for key in required if key not in data]
    if missing:
        errors.append(f"{label} missing required fields: {missing}")


def _validate_string_list(
    entry: dict[str, Any],
    key: str,
    label: str,
    errors: list[str],
    nonempty: bool = False,
) -> None:
    value = entry.get(key)
    if not isinstance(value, list) or any(not isinstance(item, str) or not item for item in value):
        errors.append(f"{label}.{key} must be a list of non-empty strings")
    elif nonempty and not value:
        errors.append(f"{label}.{key} must not be empty")
    elif len(value) != len(set(value)):
        errors.append(f"{label}.{key} must not contain duplicates")


def _validate_target_pattern(target: str, effects: list[str]) -> str | None:
    try:
        normalized = _normalize_logical_path(target)
    except RegistryError as exc:
        return str(exc)
    if normalized.startswith("@repo/"):
        if "global-spec-write" not in effects:
            return f"{target!r} requires global-spec-write"
        if not normalized.startswith(GLOBAL_TARGET_PREFIXES):
            return f"{target!r} is outside approved global prefixes"
    return None


def _normalize_logical_path(value: str) -> str:
    if not isinstance(value, str) or not value:
        raise RegistryError("artifact target must be a non-empty string")
    if "\\" in value or PureWindowsPath(value).is_absolute():
        raise RegistryError("artifact target must use a relative POSIX path")
    repo_prefixed = value.startswith("@repo/")
    logical = value[len("@repo/"):] if repo_prefixed else value
    path = PurePosixPath(logical)
    if path.is_absolute() or any(part in ("", ".", "..") for part in path.parts):
        raise RegistryError("artifact target must not be absolute or contain traversal")
    normalized = path.as_posix()
    return f"@repo/{normalized}" if repo_prefixed else normalized


def _target_matches(target: str, pattern: str) -> bool:
    if fnmatch.fnmatchcase(target, pattern):
        return True
    if pattern.endswith("/**"):
        return target.startswith(pattern[:-2])
    return False


def _effects_for_phase(effects: list[str], phase: str) -> list[str]:
    """Return registry effects filtered through the lifecycle phase policy."""
    allowed = PHASE_ALLOWED_SIDE_EFFECTS.get(phase)
    if allowed is None:
        raise RegistryError(f"unsupported phase: {phase}")
    return [effect for effect in effects if effect in allowed]


def _resolve_task_path(repo_root: Path, task_path: str) -> Path:
    normalized = _normalize_logical_path(task_path)
    if normalized.startswith("@repo/"):
        raise RegistryError("task_path cannot use a global target prefix")
    task_dir = (repo_root / normalized).resolve()
    tasks_root = (repo_root / ".trellis" / "tasks").resolve()
    _require_within(task_dir, tasks_root, "task_path must be under .trellis/tasks")
    if not task_dir.is_dir():
        raise RegistryError("task_path does not exist")
    return task_dir


def _repo_relative(path: Path, repo_root: Path) -> str:
    resolved = path.resolve()
    _require_within(resolved, repo_root.resolve(), "path is outside repository")
    return resolved.relative_to(repo_root.resolve()).as_posix()


def _require_within(path: Path, parent: Path, message: str) -> None:
    try:
        path.relative_to(parent)
    except ValueError as exc:
        raise RegistryError(message) from exc
