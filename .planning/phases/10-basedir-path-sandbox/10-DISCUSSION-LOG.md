# Phase 10: BaseDir Path Sandbox - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-06-10
**Phase:** 10-basedir-path-sandbox
**Areas discussed:** Exec cwd-absent behavior, Error message content, Validation helper placement

---

## Exec cwd-absent behavior

| Option | Description | Selected |
|--------|-------------|----------|
| Allow (only enforce explicit cwd) | Empty/absent cwd bypasses base_dir for exec. File ops sandboxed; exec is not when no cwd given. Matches literal reading of BDIR-02. | |
| Reject (require cwd when base_dir is set) | Fail with isError: true when base_dir is configured but cwd is absent. Forces callers to be explicit. Closes the sandbox gap. | ✓ |
| Default cwd to base_dir | Silently set cwd = base_dir when empty and base_dir is set. Exec runs in sandbox root without caller knowing. | |

**User's choice:** Reject (require cwd when base_dir is set)
**Notes:** BDIR-02 doesn't cover empty cwd explicitly; this closes the gap. Error: `[host X] cwd is required when base_dir is set`.

---

## Error message content

| Option | Description | Selected |
|--------|-------------|----------|
| Include base_dir value | e.g., `[host dev] path "/etc/passwd" is outside base_dir "/home/ubuntu/project"`. Consistent with exec_allowlist errors showing allowed prefixes. | ✓ |
| Omit base_dir value | e.g., `[host dev] path "/etc/passwd" is outside the configured base_dir`. More opaque. | |

**User's choice:** Include base_dir value
**Notes:** Consistency with allowlist error style was the deciding factor.

---

## Validation helper placement

| Option | Description | Selected |
|--------|-------------|----------|
| Shared helper | New internal/tools/sandbox.go with withinBaseDir() function. 6 call sites, DRY. | ✓ |
| Inline per handler | Repeat 3-line check in each of 6 handlers. Follows Phase 9 precedent (1 site). | |
| You decide | Let Claude pick at plan time. | |

**User's choice:** Shared helper
**Notes:** Phase 9 precedent was inline but that was 1 site. 6 sites justifies extraction.

---

## Claude's Discretion

None — all areas had a user decision.

## Deferred Ideas

None — discussion stayed within phase scope.
