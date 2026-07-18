# Research T8 — opencode agent contract (credentials, headless, `--` passthrough)

**Ticket:** GSA-TTS/ppp #11 — "opencode agent contract (credentials, headless, `--` passthrough)"
**Question:** How must the opencode agent be run inside the sandbox container for `ppp` v1?
**Type:** RESEARCH — decision memo, not a build.
**Date:** 2026-07-16

---

## TL;DR (decision)

1. **Credentials = env vars.** opencode reads provider API keys from **environment variables** (names supplied by models.dev, e.g. `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`), from a `.env` in the project, or from its own credentials file `~/.local/share/opencode/auth.json`. **For `ppp`, do NOT put real keys anywhere.** The `ppp` model is: the agent holds **no real key**, and the mitmproxy addon injects the real credential host-side by matching the outbound **request host**. The AI SDK's default behavior (Authorization/x-api-key header, default base URL per provider) is exactly what the addon rewrites.
2. **Headless = `opencode run "<prompt>"`.** opencode has a first-class non-interactive mode: `opencode run [message..]`. This is the invocation `ppp run` should use for scripted/headless flows. `--format json` gives machine-parseable event output; `--model provider/model` and `--agent` are available.
3. **`--` passthrough = argv appended to the `opencode` invocation** (verbatim, matching Docker `sbx`: "Args after `--` are passed straight through", e.g. `-- -s <session-id>` to resume a session). In `ppp run opencode PATH -- ARGS`, `ARGS` are the opencode CLI args/flags.
4. **Minimal image = spec §5.7 is correct**: Node.js + git + openssh + ca-certificates + sudoless user, on Ubuntu 24.04, with `npm install -g @opencode-ai/opencode`. One refinement: opencode ships **standalone native binaries** (curl installer / GitHub releases), so Node is not strictly required to *run* opencode — but §5.7's npm-global install path is valid and Node is useful for the workspace toolchain. Keep Node.

---

## 1. Credential-reading model

### How opencode reads credentials (priority order, from primary sources)

opencode is built on the **Vercel AI SDK** + **models.dev** (opencode.ai/docs/providers). Credential resolution:

1. **`opencode auth login`** → stored in `~/.local/share/opencode/auth.json` (the "credentials file"). Source: opencode.ai/docs/cli#auth, opencode.ai/docs/providers#credentials.
2. **Environment variables** — "when opencode starts up it loads the providers from the credentials file. And if there are any keys defined in your environments or a `.env` file in your project." Source: opencode.ai/docs/cli#login.
3. **`opencode.json` config** `provider.<id>.options.apiKey` (supports `{env:VAR}` interpolation) and `options.baseURL`. Source: opencode.ai/docs/providers#config.

The env-var **names** are declared per-provider by **models.dev** (the `env` array on each provider) and consumed by the provider loader (`packages/opencode/src/provider/provider.ts`: `input.env.some((item) => env[item])`). The underlying AI SDK provider (e.g. `@ai-sdk/anthropic`, `@ai-sdk/openai`) reads that env var and sets the auth header + default base URL.

### Concrete provider → env var → host → auth header table

Confirmed from **models.dev `api.json`** (env names, `api` base URL where models.dev pins one) and the **AI SDK** provider docs (default base URL + auth header). Hosts are what the mitmproxy addon must match to inject the real secret.

| Provider (opencode id) | Env var(s) | Outbound host | Auth header injected | Source |
|---|---|---|---|---|
| `anthropic` | `ANTHROPIC_API_KEY` | `api.anthropic.com` | `x-api-key: <key>` (+ `anthropic-version`, `anthropic-beta`) | models.dev; AI SDK `@ai-sdk/anthropic`; opencli-container ref (spec §12.2) |
| `openai` | `OPENAI_API_KEY` | `api.openai.com` | `Authorization: Bearer <key>` | models.dev; AI SDK `@ai-sdk/openai` (default `https://api.openai.com/v1`) |
| `google` | `GOOGLE_GENERATIVE_AI_API_KEY` \| `GOOGLE_API_KEY` \| `GEMINI_API_KEY` | `generativelanguage.googleapis.com` | `x-goog-api-key: <key>` | models.dev; AI SDK `@ai-sdk/google` |
| `github-copilot` | `GITHUB_TOKEN` (OAuth device flow preferred) | `api.githubcopilot.com` | `Authorization: Bearer <token>` | models.dev (`api: https://api.githubcopilot.com`); opencode.ai/docs/providers#github-copilot |
| `groq` | `GROQ_API_KEY` | `api.groq.com` | `Authorization: Bearer <key>` | models.dev; AI SDK `@ai-sdk/groq` |
| `mistral` | `MISTRAL_API_KEY` | `api.mistral.ai` | `Authorization: Bearer <key>` | models.dev; AI SDK `@ai-sdk/mistral` |
| `openrouter` | `OPENROUTER_API_KEY` | `openrouter.ai` (`/api/v1`) | `Authorization: Bearer <key>` (+ `HTTP-Referer`, `X-Title`) | models.dev (`api: https://openrouter.ai/api/v1`); provider.ts `openrouter` custom loader |
| `xai` | `XAI_API_KEY` | `api.x.ai` | `Authorization: Bearer <key>` | models.dev; AI SDK `@ai-sdk/xai` |
| `nebius` | `NEBIUS_API_KEY` | `api.tokenfactory.nebius.com` (`/v1`) | `Authorization: Bearer <key>` | models.dev (`api: https://api.tokenfactory.nebius.com/v1`) |

Notes:
- **`usai` (GSA USAi)** is a `ppp`-specific service (spec §5.6/§6.18). It is **not** a models.dev provider. To use it with opencode you configure a **custom provider** in `opencode.json` (`npm: @ai-sdk/openai-compatible`, `options.baseURL: https://api.gsa.usai.gov/...`). The addon matches on `--host api.gsa.usai.gov` (as `ppp secret set usai --host …` already dictates) and injects `Authorization: Bearer <key>`. Confirm the exact USAi host + header scheme against USAi's own docs before shipping.
- Providers with `null` models.dev `api` (anthropic, openai, google, groq, mistral, xai) get their default base URL from the **bundled AI SDK package**, not from models.dev — the hosts above are the AI SDK defaults. `baseURL` can be overridden in config, but for `ppp` we want the **default** hosts so the addon's host allowlist is deterministic.

### Implication for the mitmproxy addon (spec §5.4 secret injection)

The addon's host→service→header map should be seeded from the table above:
- `api.anthropic.com` → inject `x-api-key` (NOT `Authorization`) — Anthropic is the one that differs.
- `api.openai.com`, `api.groq.com`, `api.mistral.ai`, `api.x.ai`, `openrouter.ai`, `api.githubcopilot.com`, `api.tokenfactory.nebius.com` → inject `Authorization: Bearer`.
- `generativelanguage.googleapis.com` → inject `x-goog-api-key`.

The agent must NOT hold real keys. Two enforcement points:
1. The container's provider env vars can carry a **placeholder** (or be unset). opencode only needs *a* credential present to *offer/select* the provider; the placeholder satisfies presence, and the addon swaps it for the real value host-side (this is exactly opencli-container's `token_replacer` / placeholder model, spec §12.2). Simplest robust path: leave the real env unset and set the provider via `opencode.json` with a placeholder `apiKey`, then have the addon **add** the header on matching hosts and **strip** any client-supplied key.
2. `auth.json` must never be written with a real key inside the sandbox.

---

## 2. Headless / non-interactive invocation

**Decision: use `opencode run`.**

- `opencode` (no args) launches the **TUI** — unsuitable for `ppp run` scripted flows. Source: opencode.ai/docs/cli (Overview), opencode.ai/docs/tui.
- `opencode run [message..]` — "Run opencode in non-interactive mode by passing a prompt directly … useful for scripting, automation, or when you want a quick answer without launching the full TUI." Source: opencode.ai/docs/cli#run-1.

Relevant `run` flags for `ppp`:
- `--format default|json` — `json` emits raw JSON events (machine-parseable; good for `ppp` to stream/parse).
- `--model`/`-m provider/model`, `--agent`, `--session`/`-s`, `--continue`/`-c`, `--dir` (working directory).
- `--attach <url>` — attach to a running `opencode serve` to avoid MCP cold-boot (optional optimization; not needed for v1's one-shot-per-VM model).

**Note the Docker `sbx` contract difference:** Docker's `sbx run opencode` launches the **TUI** by default ("OpenCode launches in TUI mode by default"). `ppp` should NOT blindly copy that for a headless/`ppp run "<prompt>"` UX — for an interactive attached terminal (`podman run -i -t`), plain `opencode` (TUI) is fine and matches `sbx`; for a scripted prompt, `ppp` should invoke `opencode run "<prompt>"`. Recommendation: **`ppp run opencode PATH` with a prompt arg → `opencode run "<prompt>"`; without a prompt and with a TTY → `opencode` (TUI).** This is a `ppp` UX decision to record; both map cleanly onto the same container.

Source for `sbx` default: docs.docker.com/ai/sandboxes/agents/opencode ("Default startup command: The sandbox runs `opencode` with no implicit flags").

---

## 3. `--` passthrough semantics

**Decision: everything after `--` is appended verbatim as opencode CLI argv.**

Docker `sbx` (the operational surface `ppp` clones) states plainly: *"The sandbox runs `opencode` with no implicit flags. Args after `--` are passed straight through. For example, to resume an existing session: `sbx run opencode -- -s <session-id>`."* Source: docs.docker.com/ai/sandboxes/agents/opencode#default-startup-command.

So for `ppp run opencode PATH [PATH...] -- ARGS...`:
- `PATH...` = workspace mount(s) (spec §6.1).
- `ARGS...` = opencode flags/subcommand args, appended to the container's `opencode` invocation. Examples: `-- -s <session-id>` (resume), `-- run "<prompt>" --format json`, `-- --model anthropic/claude-sonnet-4-5`.

This matches spec §6.1 step 3.j: `podman run … <image> opencode <AGENT_ARGS...>`. **`AGENT_ARGS` IS the `--` passthrough.** No translation/interpretation by `ppp` — pass the slice straight to `exec`/`podman run` as argv (never build a shell string; AGENTS.md coding-standards + universal contract).

Residual detail: because `ppp` may itself want to inject `run "<prompt>"` (§2), decide precedence: if the user provides `--` args, `ppp` should treat them as the full opencode arg list and NOT also inject its own `run`. Simplest rule: **if `--` args are present, use them verbatim; otherwise `ppp` synthesizes `run "<prompt>"` or bare `opencode`.**

---

## 4. Minimal container image (cross-check with spec §5.7)

Spec §5.7 says: image built from a Containerfile layering **Node.js, git, openssh, ca-certificates, and a sudoless `ppp` user onto Ubuntu 24.04**, install via `npm install -g @opencode-ai/opencode`, env `OPENCODE_SANDBOX=1`. Published to GHCR.

**Verdict: §5.7 is accurate and sufficient.** Cross-checks:

- **Install method** — opencode publishes `opencode-ai` on npm (`npm install -g opencode-ai`), plus a curl installer, Homebrew tap, and standalone GitHub-release binaries / `ghcr.io/anomalyco/opencode` Docker image. Source: opencode.ai/docs (Install). §5.7's `@opencode-ai/opencode` package name should be verified — docs show the global package as **`opencode-ai`** (`npm install -g opencode-ai`). *Flag: confirm/repin the exact npm package name; docs consistently show `opencode-ai`.*
- **Node** — required only if installing via npm. opencode itself ships as a native binary, so a Node-free image is possible using the standalone binary. Keep Node for workspace toolchain + the documented npm install path. **No hard version pin in docs**; use current Node LTS (≥20). *Residual: no authoritative minimum Node version published; pin to LTS and test.*
- **git** — needed for `--clone` (§6.1.h) and normal agent git ops. Keep.
- **openssh** — needed for git-over-SSH and matches `sbx` model. Keep.
- **ca-certificates** — REQUIRED so the guest trusts the mitmproxy CA (the CA is imported at the VM layer via `--import-native-ca` + provision `update-ca-trust`, §5.2). The **container** also needs a CA bundle; note opencode/Node honor `NODE_EXTRA_CA_CERTS` (opencode.ai/docs/network#custom-certificates) as a belt-and-suspenders path if system trust is insufficient inside the container.
- **sudoless user** — matches §5.7 and the sandbox threat model. IMPORTANT interaction with spec §3.1/§13.10: the **sandbox identity anchor is the WG listen port, not the inner IP**, specifically because a sudo-capable agent could reassign `wg0`. A **sudoless** container user reduces (but the VM-level provisioning still runs as root) — keep the container user sudoless.
- **Proxy env** — opencode respects `HTTPS_PROXY`/`HTTP_PROXY`/`NO_PROXY` (opencode.ai/docs/network). `ppp` uses **transparent** WireGuard interception, so these are NOT needed and should be left unset (setting them would be the weaker explicit-proxy model, cf. spec §12.4 leash). Confirm the TUI's local-server loopback (`localhost`) is never proxied — moot under transparent mode.

Suggested minimal Containerfile contents (aligns with §5.7):
```
FROM ubuntu:24.04
RUN apt-get update && apt-get install -y --no-install-recommends \
      nodejs npm git openssh-client ca-certificates curl \
    && rm -rf /var/lib/apt/lists/*
RUN npm install -g opencode-ai        # verify exact package name
RUN useradd -m -s /bin/bash ppp       # sudoless
USER ppp
ENV OPENCODE_SANDBOX=1
# no *_PROXY env (transparent interception); no baked API keys
```

---

## Cited sources (URLs)

- opencode CLI (run/serve/auth/agent, env vars): https://opencode.ai/docs/cli
- opencode Providers (credentials file, `.env`, `baseURL`, provider directory, GitHub Copilot device flow): https://opencode.ai/docs/providers
- opencode Network (HTTPS_PROXY/NO_PROXY, NODE_EXTRA_CA_CERTS): https://opencode.ai/docs/network
- opencode Intro (install: npm `opencode-ai`, curl, brew, standalone binary, `ghcr.io/anomalyco/opencode`): https://opencode.ai/docs/
- opencode provider loader source (env-var presence check, per-provider header customization, anthropic `x-api-key`/`anthropic-beta`, openrouter/nvidia referer headers): https://github.com/anomalyco/opencode → `packages/opencode/src/provider/provider.ts`
- models.dev provider registry (authoritative env-var names + pinned base URLs): https://models.dev/api.json
- AI SDK provider defaults (OpenAI default base URL `https://api.openai.com/v1`, `OPENAI_API_KEY` → Authorization header): https://ai-sdk.dev/providers/ai-sdk-providers/openai
- Docker sbx opencode agent contract (`sbx run opencode`, TUI default, "Args after `--` are passed straight through", `-- -s <session-id>`, template `docker/sandbox-templates:opencode`, credential injection via proxy): https://docs.docker.com/ai/sandboxes/agents/opencode/
- ppp spec §5.4 (addon secret injection), §5.6/§6.18 (services incl. usai), §5.7 (image), §6.1 (run + AGENT_ARGS), §12.2 (opencli-container anthropic x-api-key reference): `docs/explorations/ppp-spec.md`

## Residual uncertainty

1. **Exact npm package name.** §5.7 says `@opencode-ai/opencode`; opencode docs consistently show `opencode-ai`. Verify before building the image (a `docker run ghcr.io/anomalyco/opencode` base is a zero-ambiguity alternative).
2. **Placeholder-vs-unset credential mechanics.** Need a live test: does opencode *offer/select* a provider when only a placeholder env/config key is present (so the addon can swap it), or does it validate the key shape/reject? opencli-container proves the placeholder-swap pattern works with the AI SDK header path, but confirm end-to-end with opencode specifically.
3. **Anthropic auth via OAuth vs API key.** opencode supports Claude Pro/Max OAuth (browser flow) — incompatible with headless secret-injection. `ppp` must force the **API-key** path (env/`x-api-key`) and avoid OAuth providers for headless runs. Same caveat for OpenAI ChatGPT-Plus OAuth, GitHub Copilot device flow, xAI OAuth.
4. **USAi host + header scheme.** Not a models.dev provider; the exact `api.gsa.usai.gov` base URL, path, and auth header must be confirmed against USAi docs, then wired as a custom OpenAI-compatible provider + addon rule.
5. **No published minimum Node version** for opencode; pin to Node LTS and validate.
6. **TUI default vs headless UX decision** (§2) is a `ppp` product choice, not an opencode constraint — record it in the run-command design.
