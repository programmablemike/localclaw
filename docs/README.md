---
title: "Documentation"
description: "Entry point to the localclaw docs: how the Diátaxis layout works and how to find or add the right kind of page."
diataxis: index
status: stable
last_reviewed: 2026-09-20
tags: [docs, diataxis]
related:
  - llms.txt
  - ../AGENTS.md
---

# Documentation

Everything under `docs/` follows the [Diátaxis](https://diataxis.fr) framework.
Diátaxis separates documentation into four kinds, each answering a different
reader need. Keeping them apart makes each page easier to write, easier to
find, and easier to trust.

|                                        | Serves **learning** (acquiring skill) | Serves **work** (applying skill) |
| -------------------------------------- | ------------------------------------- | -------------------------------- |
| **Practical** (informs what to do)     | [Tutorials](tutorials/README.md)      | [How-to guides](how-to/README.md) |
| **Theoretical** (informs what to know) | [Explanation](explanation/README.md)  | [Reference](reference/README.md) |

## Which section?

Ask two questions about the content:

1. Does it tell the reader **what to do** (action) or **what to know**
   (cognition)?
2. Does it serve someone **learning** the subject or someone **working** with
   it?

| Action or cognition | Learning or working | Section        | Reads like         |
| ------------------- | ------------------- | -------------- | ------------------ |
| Action              | Learning            | `tutorials/`   | a lesson           |
| Action              | Working             | `how-to/`      | a recipe           |
| Cognition           | Working             | `reference/`   | a dictionary entry |
| Cognition           | Learning            | `explanation/` | an essay           |

If a draft answers both ways, it is two pages. Split it and cross-link them.

## Sections

- [`tutorials/`](tutorials/README.md): lessons that take a newcomer through a
  complete exercise that is guaranteed to work.
- [`how-to/`](how-to/README.md): steps that solve one real task for a reader
  who already knows the basics.
- [`reference/`](reference/README.md): austere, accurate descriptions of how
  things are. Describe, do not instruct.
- [`explanation/`](explanation/README.md): discursive discussion of why things
  are the way they are, including trade-offs and alternatives.

## For agents and tools

- [`llms.txt`](llms.txt) is a curated index of every page in
  [llms.txt](https://llmstxt.org) format. Read it first to find the right page
  without opening each file.
- Every Markdown page begins with a YAML frontmatter preamble. The fields are
  defined in [`reference/frontmatter-schema.md`](reference/frontmatter-schema.md).
- To add a page, follow [`how-to/add-a-document.md`](how-to/add-a-document.md)
  and start from a skeleton in [`_templates/`](_templates/README.md).
- Contributor rules, including the requirement to use this structure, are in
  [`AGENTS.md`](../AGENTS.md) at the repository root.

## Conventions

- Filenames are lowercase kebab-case and end in `.md`.
- One topic per page and one Diátaxis type per page.
- Links between pages are relative paths.
- Each section's `README.md` lists every page in that section, and `llms.txt`
  lists every page in the tree. A page that is missing from either does not
  exist as far as tooling is concerned.
