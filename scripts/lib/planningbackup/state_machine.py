"""Fail-closed restore state machine shared by the CLI and safety tests."""

from __future__ import annotations

from dataclasses import dataclass, field

RESTORE_STAGES = (
    "preflight",
    "stop_writes",
    "replace_database",
    "replace_avatar",
    "replace_planning_media",
    "migrate",
    "validate",
    "reopen",
    "verified",
)
DESTRUCTIVE_STAGES = frozenset({
    "replace_database",
    "replace_avatar",
    "replace_planning_media",
    "migrate",
    "validate",
    "reopen",
})


class StateError(Exception):
    """An invalid restore transition."""


@dataclass
class RestoreStateMachine:
    stage: str = "preflight"
    destructive_authorized: bool = False
    writers_stopped: bool = False
    history: list[str] = field(default_factory=lambda: ["preflight"])
    failure_key: str | None = None

    def authorize(self, allowed: bool) -> None:
        if self.stage != "preflight":
            raise StateError("preflight-already-advanced")
        self.destructive_authorized = bool(allowed)
        if not self.destructive_authorized:
            raise StateError("preflight-not-authorized")

    def advance(self, next_stage: str) -> None:
        if next_stage not in RESTORE_STAGES:
            raise StateError("unknown-stage")
        if self.stage == "failed_restore_stopped":
            raise StateError("failed-restore-stopped")
        current_index = RESTORE_STAGES.index(self.stage)
        expected_index = current_index + 1
        if expected_index >= len(RESTORE_STAGES) or RESTORE_STAGES[expected_index] != next_stage:
            raise StateError("stage-order")
        if self.stage == "preflight" and not self.destructive_authorized:
            raise StateError("preflight-not-authorized")
        if next_stage == "stop_writes":
            self.writers_stopped = True
        if next_stage == "reopen" and not self.writers_stopped:
            raise StateError("writers-not-stopped")
        self.stage = next_stage
        self.history.append(next_stage)

    def fail(self, reason_key: str) -> None:
        if not reason_key:
            raise StateError("failure-reason-required")
        if self.stage in DESTRUCTIVE_STAGES or self.writers_stopped:
            self.stage = "failed_restore_stopped"
            self.writers_stopped = True
        else:
            self.stage = "failed_preflight"
        self.failure_key = reason_key
        self.history.append(self.stage)

    @property
    def terminal(self) -> bool:
        return self.stage in {"verified", "failed_preflight", "failed_restore_stopped"}

    def as_dict(self) -> dict[str, object]:
        return {
            "schema": "planning-restore-state-v1",
            "stage": self.stage,
            "destructive_authorized": self.destructive_authorized,
            "writers_stopped": self.writers_stopped,
            "history": list(self.history),
            "failure_key": self.failure_key,
        }
