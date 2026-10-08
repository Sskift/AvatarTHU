# AvatarTHU development

- Develop, commit and push directly on `main`. Do not create development branches or pull requests unless the user explicitly asks.
- Runtime and course data belong in `~/.local/share/avatarthu` on macOS, with `~/.avatarthu` as the friendly symlink. Windows uses `%USERPROFILE%\.avatarthu`. Never store runtime/course data in Git.
- AvatarTHU is a native Go program. Do not reintroduce a Python runtime, venv, Selenium or an interpreter-bundling installer.
- Keep the successful Feishu receipt durable before marking an announcement read.
- Keep homework submissions simple: one archive layer only. One extraction exposes the report, runnable program and source files in a clear directory layout. Do not nest delivery archives or duplicate versions; preserve required relative paths, executable permissions and native document/runtime data formats. Keep editing downloads separate from the final submission layout.
- Follow `docs/directory-layout.md`: each completed assignment root has exactly `submission/`, `submission.zip`, `workspace/`, and `presentation/`. Use English ASCII names for all new files and intermediate paths. Keep the minimal actual submission separate from presentation/editing downloads via `submission_files`; put execution state, drafts and history in `workspace/`, and display artifacts and reviews in `presentation/`. Preserve legacy paths, frozen bytes and receipts; never overwrite modified final directories or silently rebind cards.
- Let users independently select the writer and reviewer harness, each from Claude or Codex, including the same harness for both roles. Each stage uses a fresh process/session. Reviewers receive assignment materials and current deliverables, never writer conversations, self-assessment or previous review verdicts.
- Use each CLI's default model configuration. Do not override, compare or constrain their configured models.
- Rejected reviews return comments to the writer for another attempt. Preserve every completed review in local and Feishu documents; do not add automated homework grading or a contract-test framework.
- Only `internal/app/actions.go` may invoke the real homework upload, following the owner's current-version card action. Preserve version, message, nonce and artifact hash validation. Unknown submission results must never retry automatically.
- Mock uploads in tests. Real submission tests require the user's explicit decision on a homework card.
- Validate with `go test ./...` and `go vet ./...`. Platform or process changes also need macOS/Windows CI and an appropriate build check.
- Preserve old state and card receipts during upgrades. Legacy JSON fingerprints and unchanged material file bytes are compatibility-sensitive.
- Keepalive is every 10 minutes in the scheduler's Go process; course polling defaults to 12 hours and is configurable.
- Retain AutoThu's MIT license and upstream provenance in `THIRD_PARTY.md`. Distributed archives include third-party licenses.
