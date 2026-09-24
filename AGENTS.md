# AvatarTHU development

- Runtime and course data belong in `~/.local/share/avatarthu`, never in Git.
- Keep the successful Feishu receipt durable before marking an announcement read.
- Support two cross-review modes: Claude writes / Codex reviews, or Codex writes / Claude reviews. Each stage uses a fresh process and session. Reviewers receive the assignment and current deliverables, never writer conversations, self-assessment or earlier review verdicts.
- Use each CLI's default model configuration. Do not override, compare or constrain the two configured models.
- A rejected review returns its comments to the writer for a new attempt. Preserve every completed review in the local and Feishu review documents; do not add a separate automated grading or contract-test framework.
- Only `loop.actions` may trigger real homework uploads, following the owner's current-version card action.
- Preserve the version, message, nonce, and artifact hash checks. Unknown submission results must never retry automatically.
- Mock uploads in tests. Real submission tests require the user's explicit decision on a homework card.
- Validate with the runtime Python: `~/.local/share/avatarthu/.venv/bin/python -m unittest discover -s tests -v`.
- Vendored AutoThu code retains its MIT license and upstream provenance in `THIRD_PARTY.md`.
