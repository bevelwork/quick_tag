## 1.2 (2025-01-26)

- Refactor: Integrate `quick_color` module for ANSI colors, spinner, and utilities.
  - Import `quick_color` as `qc` and use `qc.Color*` directly throughout.
  - Replace custom progress throbber with `qc.WithProgress` and `qc.NewSpinner`.
  - Remove duplicated color constants and unused helpers.
- Build: Add module replacement to use local `quick_color` during development.

## 1.1 (2025-01-26)

- Add: Undo capability for the last tagging run with confirmation and detailed output.
- Enhance: Suggested names for ENIs/volumes/instances with batch lookups for AMI and instance names.
- Improve: History persisted to `~/.quick-tag.yml` including run IDs and timestamps.

## 1.0 (2025-01-26)

- Initial release of `quick_tag` for scanning and tagging EC2 instances, EBS volumes, and ENIs.

