---
title: "Explanation"
description: "Index of understanding-oriented discussion. Explanation pages say why things are the way they are, with context, trade-offs and alternatives."
diataxis: index
status: stable
last_reviewed: 2026-09-20
tags: [docs, diataxis, explanation]
related:
  - ../README.md
  - ../reference/README.md
---

# Explanation

Explanation is a **discussion**. It serves someone who wants to understand:
why the system is the way it is, what alternatives exist, how the pieces fit
together. It is read away from the keyboard.

## What belongs here

- Background, context and history.
- Design decisions and the trade-offs behind them.
- Comparisons with alternatives that were considered.
- Connections between concepts that no single reference page covers.

## What does not belong here

- Steps to follow. That is a [tutorial](../tutorials/README.md) or a
  [how-to guide](../how-to/README.md).
- Exhaustive facts. That is [reference](../reference/README.md).

## Writing guidance

- Title it as a topic or a question: "Why Diátaxis", "About the release
  model".
- Discursive prose is welcome here. This is the one section where it is.
- Where a statement is opinion or uncertain, say so.
- Link to reference for facts and to how-to guides for actions rather than
  repeating them.

## Documents

- [Why Diátaxis](why-diataxis.md): The reasoning behind the four-way split,
  and how frontmatter and llms.txt make that structure discoverable by agents.
- [CLI architecture](cli-architecture.md): Why lclaw is layered as presentation, domain and data, which dependencies it accepts, and how the doctor command proves the design.
- [Secrets management](secrets-management.md): Why secrets live in a dedicated macOS keychain, how lclaw injects them into each Podman machine as Kubernetes-shaped secrets, and what the secrets commands own.
