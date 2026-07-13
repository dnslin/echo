# Domain Docs

How the engineering skills should consume this repo's domain documentation when exploring the codebase.

## Before exploring, read these

- **`CONTEXT.md`** at the repo root, or
- **`CONTEXT-MAP.md`** at the repo root if it exists — it points at one `CONTEXT.md` per context. Read each one relevant to the topic.
- **`docs/adr/`** — read ADRs that touch the area you're about to work in. In multi-context repos, also check `src/<context>/docs/adr/` for context-scoped decisions.

If any of these files don't exist, **proceed silently**. Don't flag their absence; don't suggest creating them upfront. The `/domain-modeling` skill creates them lazily when terms or decisions actually get resolved.

## File structure

Single-context repo (this repository):

/
├── CONTEXT.md
├── docs/adr/
└── src/

Multi-context repos use `CONTEXT-MAP.md` plus one `CONTEXT.md` per context.

## Use the glossary's vocabulary

When naming a domain concept in an issue, refactor proposal, hypothesis, or test name, use the term defined in `CONTEXT.md`. Avoid synonyms the glossary explicitly excludes.

A missing needed term means either reconsider invented language or record the genuine gap for `/domain-modeling`.

## Flag ADR conflicts

If output contradicts an existing ADR, surface it explicitly rather than silently overriding:

> _Contradicts ADR-0007 — but worth reopening because…_
