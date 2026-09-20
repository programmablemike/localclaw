---
title: "llms.txt index format"
description: "Location, structure and entry format of docs/llms.txt, the curated index that lets agents find pages without reading every file."
diataxis: reference
status: stable
last_reviewed: 2026-09-20
tags: [docs, llms-txt, agents]
related:
  - frontmatter-schema.md
  - ../how-to/add-a-document.md
  - ../explanation/why-diataxis.md
---

# llms.txt index format

## Location

`docs/llms.txt`. When `docs/` is served as a website this path resolves to
`/llms.txt`, the location defined by the
[llms.txt specification](https://llmstxt.org).

## Structure

The file is Markdown with a fixed shape, in this order.

| Part                  | Required | Content                                                                                  |
| --------------------- | -------- | ---------------------------------------------------------------------------------------- |
| `# <project name>`    | yes      | Exactly one H1.                                                                          |
| `> <summary>`         | yes      | One blockquote stating what the docs are and how they are organised.                    |
| Prose paragraphs      | no       | Context an agent needs before following links. No headings.                              |
| `## <section>` blocks | yes      | One H2 per group below, each containing a file list.                                     |

The H2 sections appear in this order and with these exact names:

| Section         | Contains                                                              |
| --------------- | --------------------------------------------------------------------- |
| `Start here`    | `../README.md`, `README.md` and `../AGENTS.md`.                       |
| `Tutorials`     | Pages with `diataxis: tutorial`, preceded by `tutorials/README.md`.   |
| `How-to guides` | Pages with `diataxis: how-to`, preceded by `how-to/README.md`.        |
| `Reference`     | Pages with `diataxis: reference`, preceded by `reference/README.md`.  |
| `Explanation`   | Pages with `diataxis: explanation`, preceded by `explanation/README.md`. |
| `Optional`      | `_templates/README.md` and any material a consumer may skip when short on context. |

## Entry format

Each file list item has the form:

```text
- [<title>](<relative path>): <description>
```

| Part              | Rule                                                                   |
| ----------------- | ---------------------------------------------------------------------- |
| `<title>`         | Identical to the page's frontmatter `title`.                           |
| `<relative path>` | Relative to `docs/llms.txt`. Points at a `.md` file, never a directory. Forward slashes, no leading `./`. |
| `<description>`   | Identical to the page's frontmatter `description`.                     |

## Constraints

- Every Markdown page under `docs/` appears exactly once.
- A page appears under the H2 that matches its `diataxis` value.
- Within a section, the section `README.md` is listed first; other pages
  follow in alphabetical order of path.
