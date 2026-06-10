---
phase: 10
slug: basedir-path-sandbox
status: verified
threats_open: 0
asvs_level: 1
created: 2026-06-10
---

# Phase 10 — Security

> Per-phase security contract: threat register, accepted risks, and audit trail.

---

## Trust Boundaries

| Boundary | Description | Data Crossing |
|----------|-------------|---------------|
| MCP client → tool handlers | Claude supplies arbitrary `path`/`remote_path`/`cwd` strings; untrusted input crosses into remote filesystem and command operations | Remote paths and working directories (untrusted) |

---

## Threat Register

| Threat ID | Category | Component | Disposition | Mitigation | Status |
|-----------|----------|-----------|-------------|------------|--------|
| T-10-01 | Elevation of Privilege | withinBaseDir lexical check | mitigate | `sandbox.go:29-30` — `path.Clean` applied to both operands before prefix check; traversal sequences collapse before comparison (BDIR-03) | closed |
| T-10-02 | Elevation of Privilege | prefix-collision (`/home/user` vs `/home/username`) | mitigate | `sandbox.go:46` — `strings.HasPrefix(cleanPath, cleanBase+"/")` trailing-slash anchor prevents sibling-directory false positives | closed |
| T-10-03 | Tampering | symlink whose target escapes base_dir | accept | See Accepted Risks Log (R-10-01) | closed |
| T-10-04 | Elevation of Privilege | readFileHandler/listDirHandler/writeFileHandler path param | mitigate | `file_read.go:47-56`, `dir.go:120-128`, `file_write.go:47-55` — `withinBaseDir` guard after `resolveExecutor`, before any SSH I/O (BDIR-01) | closed |
| T-10-05 | Information Disclosure | error message reveals base_dir | accept | See Accepted Risks Log (R-10-02) | closed |
| T-10-06 | Tampering | write guard ordering vs allow_overwrite | mitigate | `file_write.go:47` (withinBaseDir) before `file_write.go:64` (AllowOverwrite) — out-of-sandbox writes never reach remote `test -e` (D-07) | closed |
| T-10-07 | Elevation of Privilege | uploadHandler/downloadHandler RemotePath | mitigate | `transfer.go:65` (upload) and `transfer.go:132` (download) — `withinBaseDir` guard on `in.RemotePath` before AllowOverwrite at lines 79/145 (BDIR-01, D-07) | closed |
| T-10-08 | Elevation of Privilege | execHandler empty cwd with base_dir set | mitigate | `exec.go:104-111` — explicit rejection `cwd is required when base_dir is set`; after allowlist, before `RunCommand` (BDIR-02, D-01) | closed |
| T-10-09 | Elevation of Privilege | execHandler non-empty cwd | mitigate | `exec.go:115-124` — `withinBaseDir(baseDir, in.Cwd)` guard; after allowlist, before `RunCommand` (BDIR-02, D-02) | closed |
| T-10-SC | Tampering | supply chain (new dependencies) | mitigate | `sandbox.go` imports only stdlib `"path"` and `"strings"`; no new external packages added in this phase | closed |

*Status: open · closed*
*Disposition: mitigate (implementation required) · accept (documented risk) · transfer (third-party)*

---

## Accepted Risks Log

| Risk ID | Threat Ref | Rationale | Accepted By | Date |
|---------|------------|-----------|-------------|------|
| R-10-01 | T-10-03 | Lexical check intentionally does not resolve remote symlinks — resolution would add an SSH round-trip per operation plus a TOCTOU window. Matches SAFE-01 precedent. Documented in `sandbox.go:11-16` function comment and in all six affected tool descriptions in `register.go` (lines 32, 40, 45, 53, 58, 63). Residual exposure: a symlink planted inside base_dir can point outside it. | plan-time (BDIR-03) + audit | 2026-06-10 |
| R-10-02 | T-10-05 | Rejection errors surface the configured `base_dir` value so the MCP client can self-correct (D-03). `base_dir` is operator-configured, not a secret; the daemon is local-only (Unix socket, no network exposure). | plan-time (D-03) + audit | 2026-06-10 |

*Accepted risks do not resurface in future audit runs.*

---

## Security Audit Trail

| Audit Date | Threats Total | Closed | Open | Run By |
|------------|---------------|--------|------|--------|
| 2026-06-10 | 10 | 10 | 0 | gsd-security-auditor |

### Audit Notes — 2026-06-10

- Register authored at plan time across plans 10-01, 10-02, 10-03; auditor verified mitigations only (no new-threat scan).
- All three SUMMARY.md `## Threat Flags` sections report no unregistered threat surface.
- Symlink-limitation documentation count confirmed: `grep -c 'symlinks on the remote are not resolved' internal/tools/register.go` = 6.

---

## Sign-Off

- [x] All threats have a disposition (mitigate / accept / transfer)
- [x] Accepted risks documented in Accepted Risks Log
- [x] `threats_open: 0` confirmed
- [x] `status: verified` set in frontmatter

**Approval:** verified 2026-06-10
