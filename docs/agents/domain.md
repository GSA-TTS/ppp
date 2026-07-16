# Domain Docs

How the engineering skills should consume this repo's domain documentation when exploring the codebase.

Consumer skills that read these: `improve-codebase-architecture`, `diagnosing-bugs`, `tdd`, `codebase-design`, `implement`, `to-tickets`, `to-spec`. Producer skills that create/update them: `grill-with-docs`, `domain-modeling`.

## Before exploring, read these

- **`CONTEXT.md`** at the repo root (see also `CONTEXT-GUIDE.md`, which explains how this repo's context docs are organized).
- **`docs/decisions/`** — read Architecture Decision Records (ADRs) that touch the area you're about to work in. **This repo uses `docs/decisions/`, not `docs/adr/`.**
- **`docs/explorations/ppp-spec.md`** — the authoritative design spec for the `ppp` runtime. Treat it as the primary source of domain language and architecture until a `CONTEXT.md` glossary supersedes specific terms.

If any of these files don't exist, **proceed silently**. Don't flag their absence; don't suggest creating them upfront. The producer skills (`grill-with-docs`, `domain-modeling`) create them lazily when terms or decisions actually get resolved.

## File structure

Single-context repo:

```
/
├── CONTEXT.md                (lazily created)
├── CONTEXT-GUIDE.md          (already present — how context docs are organized)
├── docs/
│   ├── explorations/
│   │   └── ppp-spec.md       (authoritative design spec)
│   └── decisions/            ← ADRs live HERE (not docs/adr/)
│       ├── 0001-*.md
│       └── 0002-*.md
└── (cmd/, internal/, assets/ — see docs/explorations/ppp-spec.md §7 repo layout)
```

## Use the glossary's vocabulary

When your output names a domain concept (in an issue title, a refactor proposal, a hypothesis, a test name), use the term as defined in `CONTEXT.md` — and, until that exists, the terminology established in `docs/explorations/ppp-spec.md` (e.g. "sandbox", "daemon" = the single mitmdump process, "WG instance", "inner tunnel IP", "sandbox identification by listen port", "kit", "template", "provision script"). Don't drift to synonyms the spec avoids.

If the concept you need isn't in the spec or glossary yet, that's a signal — either you're inventing language the project doesn't use (reconsider) or there's a real gap (note it for the docs producer skill).

## Flag ADR conflicts

If your output contradicts an existing ADR in `docs/decisions/`, surface it explicitly rather than silently overriding:

> _Contradicts ADR-0002 (single mitmdump process) — but worth reopening because…_

(No ADRs exist in `docs/decisions/` yet; the first ones will likely capture: the isolation backend choice — Podman Machine over Lima; the single-mitmdump/multi-WireGuard proxy model; sandbox identification by WireGuard listen port; and the non-FIPS-validated crypto acceptance for a local dev tool.)
