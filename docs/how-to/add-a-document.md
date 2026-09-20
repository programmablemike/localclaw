---
title: "Add a document"
description: "Create a page in the right Diátaxis section, fill in the frontmatter, and register it in the indexes so agents can find it."
diataxis: how-to
status: stable
last_reviewed: 2026-09-20
tags: [docs, diataxis, frontmatter, llms-txt]
related:
  - ../reference/frontmatter-schema.md
  - ../reference/llms-txt.md
  - ../explanation/why-diataxis.md
---

# Add a document

When you finish, a new page exists under `docs/` in the correct section,
carries valid frontmatter, and is listed in both its section index and
`docs/llms.txt`.

## Prerequisites

- A worktree on a branch named `docs/<topic>`, as
  [`AGENTS.md`](../../AGENTS.md) requires.
- A decided subject for the page. If you are unsure whether it is one page or
  two, it is two.

## Steps

### 1. Choose the section

Answer two questions about the content:

1. Does it tell the reader **what to do** or **what to know**?
2. Is the reader **learning** the subject or **working** with it?

| What to do or know | Learning or working | Section        | Template                    |
| ------------------ | ------------------- | -------------- | --------------------------- |
| Do                 | Learning            | `tutorials/`   | `_templates/tutorial.md`    |
| Do                 | Working             | `how-to/`      | `_templates/how-to.md`      |
| Know               | Working             | `reference/`   | `_templates/reference.md`   |
| Know               | Learning            | `explanation/` | `_templates/explanation.md` |

If the content answers both ways, stop and split it into two pages. Repeat
this guide for each.

### 2. Create the file from the template

Choose a lowercase kebab-case filename that names the topic, then copy the
matching template. For a how-to guide:

```bash
cp docs/_templates/how-to.md docs/how-to/<filename>.md
```

Name the page for its type. How-to titles are verb phrases ("Cut a release").
Tutorials read as lessons ("Getting started with ..."). Reference pages are
named for the thing described ("Frontmatter schema"). Explanation pages are
named for the topic or question ("Why Diátaxis").

### 3. Fill in the frontmatter

Replace every placeholder in the YAML block at the top of the file. The fields
and allowed values are in
[Frontmatter schema](../reference/frontmatter-schema.md). In particular:

- `diataxis` must match the directory: `tutorial`, `how-to`, `reference` or
  `explanation`.
- `description` is one sentence of at most 160 characters. You will reuse it
  verbatim in step 5.
- `last_reviewed` is today's date as `YYYY-MM-DD`.
- `status` is `draft` until the page has been checked end to end, then
  `stable`.

### 4. Write the body

Follow the HTML comments in the template, then delete them. Keep to the
section's job:

- **Tutorial:** every step concrete, expected output shown, result guaranteed
  on a clean checkout. No rationale. Link to explanation instead.
- **How-to guide:** one goal, numbered imperative steps, a verification step
  at the end. No teaching. Link to a tutorial instead.
- **Reference:** describe, do not instruct. Tables for fields and options.
  Structure mirrors the thing described.
- **Explanation:** discuss why, including trade-offs and alternatives. Link to
  reference for facts rather than repeating them.

Use relative paths for every link to another page.

### 5. Register the page

Add one list item in each of two places, using the frontmatter `title` and
`description` verbatim:

1. The `## Documents` list in the section's `README.md`:

   ```markdown
   - [<title>](<filename>.md): <description>
   ```

2. The matching `## <section>` block in [`docs/llms.txt`](../llms.txt), with
   the path relative to `docs/`:

   ```markdown
   - [<title>](how-to/<filename>.md): <description>
   ```

Add the new page to the `related` list of any existing page that should point
to it.

### 6. Verify

Run these from the repository root, substituting your filename and section.
Each line should print its `ok` message.

```bash
head -n 1 docs/how-to/<filename>.md | grep -qx -e '---' && echo "frontmatter: ok"
grep -q '^diataxis: how-to$' docs/how-to/<filename>.md && echo "diataxis: ok"
grep -q '<filename>.md' docs/how-to/README.md && echo "section index: ok"
grep -q 'how-to/<filename>.md' docs/llms.txt && echo "llms.txt: ok"
```

Then read the page once as its intended reader would. Run a tutorial start to
finish on a clean checkout.

### 7. Open the pull request

Commit with the `docs` type, for example `docs(how-to): add cut-a-release
guide`, and open the pull request as [`AGENTS.md`](../../AGENTS.md)
describes. State in the PR body which section the page belongs to and why.
