---
title: "Reference"
description: "Index of reference material. Reference pages describe the machinery accurately and austerely, structured to mirror what they describe."
diataxis: index
status: stable
last_reviewed: 2026-09-21
tags: [docs, diataxis, reference]
related:
  - ../README.md
  - ../explanation/README.md
---

# Reference

Reference is a **description**. It serves someone who is working and needs a
fact: a field name, an allowed value, a command's flags. It is consulted rather
than read, so it must be accurate, complete and predictable in structure.

## What belongs here

- Schemas, field lists, allowed values and defaults.
- Command and API signatures with every option.
- File formats and directory layouts.
- Structure that mirrors the thing described, so a reader who knows the
  subject can navigate by that knowledge.

## What does not belong here

- Instructions for reaching a goal. That is a
  [how-to guide](../how-to/README.md).
- Opinion, history or rationale. That is
  [explanation](../explanation/README.md).
- Examples that teach rather than illustrate. That is a
  [tutorial](../tutorials/README.md).

## Writing guidance

- Be austere. State facts. Do not persuade or advise.
- Use tables for fields, options and values.
- Give at most one short, illustrative example per concept.
- Keep it in step with the code. A wrong reference page is worse than none.

## Documents

- [Frontmatter schema](frontmatter-schema.md): Fields, types and allowed
  values for the YAML preamble that begins every Markdown page under docs/.
- [llms.txt index format](llms-txt.md): Location, structure and entry format
  of docs/llms.txt, the curated index that lets agents find pages without
  reading every file.
- [lclaw command reference](cli.md): Commands, global flags, environment
  variables, exit codes and the JSON output shape of the lclaw command-line
  tool.
- [Scaffold reference](scaffold.md): Layout of the lclaw scaffold directory,
  the lclaw.toml schema and validation rules, the Pod file conventions, and
  what lclaw init does to each file.
