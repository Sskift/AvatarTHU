# AvatarTHU development

- Runtime and course data belong in `~/.local/share/avatarthu`, never in Git.
- Keep the successful Feishu receipt durable before marking an announcement read.
- Use one Claude Code process per assignment attempt. Do not add a second model/reviewer stage.
- Only `loop.actions` may trigger real homework uploads, following the owner's current-version card action.
- Preserve the version, message, nonce, and artifact hash checks. Unknown submission results must never retry automatically.
- Mock uploads in tests. Real submission tests require the user's explicit decision on a homework card.
- Validate with the runtime Python: `~/.local/share/avatarthu/.venv/bin/python -m unittest discover -s tests -v`.
- Vendored AutoThu code retains its MIT license and upstream provenance in `THIRD_PARTY.md`.
