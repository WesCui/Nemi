# Nemi development instructions

- Use Bash for commands, builds, tests and Git operations, as requested by the user.
- On this Windows machine use the installed Git Bash at `C:/Program Files/Git/bin/bash.exe`; the `bash` on Windows PATH can resolve to the WSL launcher. Do not install or reconfigure WSL merely to run this project.
- Source `scripts/env.sh` from the repository root before Go commands. Never print `.env`, invite codes, sessions or model API keys.
- Use a dedicated `nemi_test` database and isolated Temporal task queues for integration tests. Do not truncate or drop a user's database.
- Keep `data`, `.cache`, `.env`, generated binaries and test output out of Git.
- Build the consumer product without private-development branding in the UI, as requested by the user. Describe demo generation, station-only reminders, pending connectors and live verification accurately; do not claim production readiness or complete dots parity without evidence.
