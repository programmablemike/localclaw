---
title: "How-to guides"
description: "Index of task-oriented guides. A how-to guide gives a competent reader the steps to accomplish one real goal."
diataxis: index
status: stable
last_reviewed: 2026-09-21
tags: [docs, diataxis, how-to]
related:
  - ../README.md
  - ../tutorials/README.md
---

# How-to guides

A how-to guide is a **recipe**. It serves someone who is already working and
has a specific goal. It assumes competence, names the goal, and gives the steps
that reach it.

## What belongs here

- One goal per page, named in the title as a verb phrase: "Add a document",
  "Cut a release".
- Steps in the order they are performed, each one an action.
- Branches only where a real task genuinely branches: "If you are on macOS
  ...".
- Links to [reference](../reference/README.md) for details and to
  [explanation](../explanation/README.md) for reasoning. Do not inline either.

## What does not belong here

- Teaching fundamentals to a newcomer. That is a
  [tutorial](../tutorials/README.md).
- Exhaustive listings of options. That is [reference](../reference/README.md).
- Paragraphs about why. That is [explanation](../explanation/README.md).

## Writing guidance

- Open with one sentence saying what the reader will have when done.
- List prerequisites briefly and assume the reader can meet them.
- Use numbered steps. Start each with an imperative verb.
- Finish with a verification step: how the reader confirms it worked.
- Do not explain. If a sentence of rationale is unavoidable, write it and link
  to the relevant explanation page.

## Documents

- [Add a document](add-a-document.md): Create a page in the right Diátaxis
  section, fill in the frontmatter, and register it in the indexes so agents
  can find it.
- [Set up a development environment](set-up-a-development-environment.md):
  Enter the Flox environment, run the check script, build lclaw from source
  and run doctor against your machine.
- [Apply a deployment by hand](apply-a-deployment-by-hand.md): Create, start and load the LocalClaw machine and one zone from the scaffold with plain podman commands, for troubleshooting or when lclaw is not enough.
- [Bring the system up and down](bring-the-system-up-and-down.md): Run lclaw up for the first time, apply an edit, check status, take one zone or everything down, and destroy the machine.
- [Manage secrets](manage-secrets.md): Create the keychain, set a provider
  API key, rotate the LiteLLM master key, and read a value to log into the
  LiteLLM dashboard.
