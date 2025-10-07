### 1.3 (2025-10-07)

- Refactor: Stop re-exporting color constants; use `qc.Color*` directly.
- Tests: Update to import `quick_color` as `qc` and reference `qc.Color*`.
- Progress: Use `qc.WithProgress`/`qc.NewSpinner` for spinners.
- Dependencies: Pull latest `github.com/bevelwork/quick_color` and tidy modules.
- Housekeeping: Ensure `go fmt` clean and tests pass.

### 1.2 (2025-01-26)

- Integrate `quick_color` module for ANSI colors and spinner utilities.
- Replace custom throbber with `qc.WithProgress` and `qc.NewSpinner`.
- Remove duplicated color constants and unused helpers.

### 1.1 (2025-01-26)

- Add undo capability for last tagging run with confirmation and detailed output.
- Improve suggested names for ENIs/volumes/instances and history persistence.

### 1.0 (2025-01-26)

- Initial release for scanning and tagging EC2 instances, EBS volumes, and ENIs.

