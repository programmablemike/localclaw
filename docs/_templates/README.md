---
title: "Document templates"
description: "Copyable skeletons, one per Diátaxis type, with placeholder frontmatter and a commented body outline."
diataxis: index
status: stable
last_reviewed: 2026-09-20
tags: [docs, templates]
related:
  - ../how-to/add-a-document.md
  - ../reference/frontmatter-schema.md
---

# Document templates

Copy one of these into the matching section and replace every placeholder. The
HTML comments in each body say what goes where. Delete them as you fill the
page in. The full procedure is in
[Add a document](../how-to/add-a-document.md).

| Template                          | Copy to             | `diataxis` value |
| --------------------------------- | ------------------- | ---------------- |
| [`tutorial.md`](tutorial.md)       | `docs/tutorials/`   | `tutorial`       |
| [`how-to.md`](how-to.md)           | `docs/how-to/`      | `how-to`         |
| [`reference.md`](reference.md)     | `docs/reference/`   | `reference`      |
| [`explanation.md`](explanation.md) | `docs/explanation/` | `explanation`    |

The four skeletons carry placeholder frontmatter by design and are exempt from
the [frontmatter schema](../reference/frontmatter-schema.md). They are listed
under `Optional` in `llms.txt` so consumers with a small context budget can
skip them.
