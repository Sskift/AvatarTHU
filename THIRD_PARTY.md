# Third-party sources

- `loop/thulearn/client.py`, `login.py`, and `keepalive.py` contain code adapted from [AutoThu](https://github.com/shiyt23/AutoThu), MIT, including our macOS one-click login and periodic keepalive contributions from [Sskift/AutoThu](https://github.com/Sskift/AutoThu/tree/feat/macos-one-click-login). Source snapshot: `b5caba55ba2a53fcd1e08db1a5fab0d01231a39f`. The original copyright and license are retained in [third_party/AutoThu-LICENSE](third_party/AutoThu-LICENSE) and copied into installed distributions.
- AvatarTHU is developed as an independent project. The adapted files use package-relative imports, AvatarTHU-owned session storage, and AvatarTHU's installer and LaunchAgents. The upstream CLI, checkout, standalone keepalive environment, Windows helpers and `thulearn2018` package are not required or bundled. No external AutoThu executable is invoked.
- Feishu support optionally invokes the official [Lark CLI](https://github.com/larksuite/cli). No Lark CLI source or binary is bundled in this repository; an existing installation is reused, or `@larksuite/cli@1.0.96` is installed through npm in the runtime directory when the owner enables Feishu.
- Initial workflow, approval validation and Card 2.0 layout adapted from the owner's local `thu-learn-loop` prototype, then converted to a single Claude Code executor.
- Network API field/endpoint references cross-checked against https://github.com/Harry-Chen/thu-learn-lib and the current Web Learning responses. No TypeScript implementation is bundled.
- Card 2.0 structure follows the installed Lark CLI component documentation. Historical layout inspiration: https://github.com/TWe1v3/Feishu-card-strong . No third-party card skill code is executed or bundled.

Course materials, student data, login sessions, credentials and generated assignments are never part of the source repository.
