# Third-party sources

- `vendor/autothu/`: AutoThu, MIT, upstream https://github.com/shiyt23/AutoThu ; local macOS login/keepalive contributions from https://github.com/Sskift/AutoThu/tree/feat/macos-one-click-login . Snapshot commit `b5caba55ba2a53fcd1e08db1a5fab0d01231a39f`. Original license is retained at `vendor/autothu/LICENSE`.
- Initial workflow, approval validation and Card 2.0 layout adapted from the owner's local `thu-learn-loop` prototype, then converted to a single Claude Code executor.
- Network API field/endpoint references cross-checked against https://github.com/Harry-Chen/thu-learn-lib and the current Web Learning responses. No TypeScript implementation is bundled.
- Card 2.0 structure follows the installed Lark CLI component documentation. Historical layout inspiration: https://github.com/TWe1v3/Feishu-card-strong . No third-party card skill code is executed or bundled.

Course materials, student data, login sessions, credentials and generated assignments are never part of the source repository.
