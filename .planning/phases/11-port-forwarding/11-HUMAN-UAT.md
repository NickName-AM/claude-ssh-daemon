---
status: partial
phase: 11-port-forwarding
source: [11-VERIFICATION.md]
started: 2026-06-10T16:38:46Z
updated: 2026-06-10T16:38:46Z
---

## Current Test

[awaiting human testing]

## Tests

### 1. Both tools visible in tools/list with PortForward=true
expected: With PortForward=true in config, ssh_forward_port and ssh_list_forwards appear in Claude Code's tool list; ssh_forward_port has destructiveHint, ssh_list_forwards is read-only
result: [pending]

### 2. Neither tool visible in tools/list with PortForward=false
expected: With PortForward=false in config, neither ssh_forward_port nor ssh_list_forwards appear in tools/list response
result: [pending]

### 3. Active forwards are killed cleanly on daemon SIGTERM
expected: KillAll() sends SIGKILL to child processes before ln.Close(); no orphaned ssh -L processes remain after daemon exit
result: [pending]

## Summary

total: 3
passed: 0
issues: 0
pending: 3
skipped: 0
blocked: 0

## Gaps
