# Codex app-server image (mode 8)

Packages the shared Codex app-server that backs `reasoning` level 8, as
**`infiniflow/codex:<version>`**, which the `codex` service in `../docker-compose.yml` pulls.

The container is mode 8's isolation boundary. It mounts no corpus and no host filesystem, so
Codex reaches every dataset through the API's MCP bridge (`/mcp/codex/<handle>`) and cannot
read the host at all. The inner Codex sandbox is therefore redundant and off
(`RAGFLOW_CODEX_SANDBOX_MODE=danger-full-access`): an unprivileged container cannot run the
read-only sandbox's `bwrap`/`landlock` helper anyway, and the image is a full userland so a
turn may run ordinary local commands (including `python3`) inside it.

| File | Purpose |
|---|---|
| `Dockerfile` | glibc image (ubuntu) with python3 and the app-server's shared libraries; copies the prebuilt binary in and runs it unprivileged. Compiles nothing at build time. |
| `entrypoint.sh` | Writes `CODEX_WS_TOKEN` (env) into the token file the CLI requires, then starts the app-server with capability-token WS auth on `0.0.0.0:8790`. |
| `build-local.sh` | Host-side: compiles the pinned Codex source tree (`CODEX_SRC`) into `codex-app-server` (git-ignored). |
| `.dockerignore` | Keeps the script and archives out of the build context. |

## Build

```bash
cd docker/codex
CODEX_SRC=/path/to/codex/codex-rs ./build-local.sh
docker build -t infiniflow/codex:<version> .
```

`build-local.sh` is the only producer of the binary. It builds `codex-cli --bin codex` and
drops it next to the Dockerfile, with the packaging profile tuned for a Codex-sized build:

- **stable rustc** — the source tree pins a channel in `rust-toolchain.toml`; packaging uses
  the installed stable toolchain instead (override with `RUSTUP_TOOLCHAIN`).
- **no LTO** — the repo's release profile enables thin LTO, which dominates the final link
  (tens of minutes, several GiB, enough to trip an out-of-memory watchdog).
- **no debuginfo**, symbols stripped — an image never symbolicates.
- **mold**, falling back to **lld** — linking a multi-hundred-MiB binary with GNU ld is the
  other slow step.
- `CARGO_BUILD_JOBS` passes through, so callers can cap parallelism when memory is tight.

`COPY` preserves the executable bit, so the Dockerfile deliberately does **not** `chmod` the
binary: a `chmod` layer would re-store the whole file and double the image. If the binary is
missing or not executable, the build fails with a clear message.

The base is glibc (the locally built binary links glibc), and the CA bundle is what lets the
app-server reach the model provider over TLS.

## Notes

- The image is **linux/amd64** only; it matches the host that built the binary. Build on the
  target architecture (or cross-compile) for anything else.
- Tags are pinned per version; no `latest` is published.
- The image carries **no** model credentials or proxy settings — the mode-8 engine declares
  the model provider per thread, and the app-server reaches it over the container's network.
- The app-server log may warn that `bubblewrap` is absent. That is expected: the sandbox mode
  above does not use it, and the container (not `bwrap`) is what bounds a turn.
