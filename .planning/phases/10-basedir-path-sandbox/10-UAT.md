---
status: complete
phase: 10-basedir-path-sandbox
source: [10-01-SUMMARY.md, 10-02-SUMMARY.md, 10-03-SUMMARY.md]
started: 2026-06-10T00:00:00Z
updated: 2026-06-10T17:35:00Z
---

## Current Test

[testing complete]

## Tests

### 1. Read file outside base_dir is rejected
expected: With base_dir configured (e.g. "/srv/app"), ssh_read_file on a path outside it (e.g. "/etc/hostname") returns error `[host X] path "..." is outside base_dir "..."` and no file content.
result: pass

### 2. Traversal path is rejected
expected: ssh_read_file (or any file tool) with a traversal path like "/srv/app/../etc/passwd" is rejected with the outside-base_dir error — the "../" does not escape the sandbox.
result: pass

### 3. Operations inside base_dir still work
expected: ssh_read_file, ssh_list_dir, and ssh_write_file on paths inside base_dir (e.g. "/srv/app/config.txt") work normally with no sandbox error.
result: pass

### 4. Upload/download outside base_dir is rejected
expected: ssh_upload_file and ssh_download_file with a remote path outside base_dir return the outside-base_dir error before any transfer happens.
result: pass

### 5. ssh_exec requires cwd when base_dir is set
expected: With base_dir set, calling ssh_exec without a cwd returns error `[host X] cwd is required when base_dir is set`. With cwd outside base_dir, returns the outside-base_dir error. With cwd inside base_dir, command runs normally.
result: pass
note: sub-cases b/c verified live; empty-cwd rejection (a) verified via TestExecBaseDirEmptyCwdRejected unit test (Claude client auto-supplies cwd)

### 6. No base_dir means unchanged behavior
expected: For a host with no base_dir configured, all tools (read, write, list, exec, upload, download) work exactly as before — no sandbox errors anywhere.
result: pass

### 7. Symlink caveat visible in tool descriptions
expected: All six file/exec tool descriptions (ssh_exec, ssh_read_file, ssh_list_dir, ssh_write_file, ssh_upload_file, ssh_download_file) mention that symlinks are not resolved and may point outside base_dir.
result: pass

## Summary

total: 7
passed: 7
issues: 0
pending: 0
skipped: 0

## Gaps

[none yet]
