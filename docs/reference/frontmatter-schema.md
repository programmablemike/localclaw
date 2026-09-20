---
title: "Frontmatter schema"
description: "Fields, types and allowed values for the YAML preamble that begins every Markdown page under docs/."
diataxis: reference
status: stable
last_reviewed: 2026-09-20
tags: [docs, frontmatter, schema]
related:
  - llms-txt.md
  - ../how-to/add-a-document.md
  - ../explanation/why-diataxis.md
---

# Frontmatter schema

Every Markdown file under `docs/` begins with a YAML block delimited by `---`
on its own line, before any other content. Tools and agents use it to classify
and index a page without parsing the body.

## Fields

| Field           | Required | Type            | Allowed values or format                                    | Purpose                                                         |
| --------------- | -------- | --------------- | ----------------------------------------------------------- | --------------------------------------------------------------- |
| `title`         | yes      | string          | Free text. Matches the page's H1.                           | Display name in indexes and `llms.txt`.                         |
| `description`   | yes      | string          | One sentence, at most 160 characters.                       | Summary used verbatim in section indexes and `llms.txt`.        |
| `diataxis`      | yes      | enum            | `tutorial`, `how-to`, `reference`, `explanation`, `index`   | The page's Diátaxis type. `index` is for a section `README.md`. |
| `status`        | yes      | enum            | `draft`, `stable`, `deprecated`                             | Lifecycle state.                                                |
| `last_reviewed` | yes      | date            | `YYYY-MM-DD`                                                | Date the content was last confirmed accurate.                   |
| `tags`          | no       | list of strings | Lowercase kebab-case words.                                 | Topic keywords for search.                                      |
| `related`       | no       | list of strings | Paths relative to the page.                                 | Cross-links to closely related pages.                           |

Fields appear in the order listed. Unknown fields are not permitted.

## Constraints

| Constraint                                                                                                        | Applies to        |
| ----------------------------------------------------------------------------------------------------------------- | ----------------- |
| `diataxis` matches the directory: `tutorials/` is `tutorial`, `how-to/` is `how-to`, `reference/` is `reference`, `explanation/` is `explanation`. | all content pages |
| `diataxis` is `index` for every `README.md` under `docs/`.                                                        | section indexes   |
| `description` is identical to the page's entry in its section `README.md` and in `llms.txt`.                     | all pages         |
| A `deprecated` page names its replacement in the first paragraph of the body.                                    | deprecated pages  |
| Filename is lowercase kebab-case and ends in `.md`.                                                               | all pages         |

## Example

```yaml
---
title: "Add a document"
description: "Create a page in the right Diátaxis section, fill in the frontmatter, and register it in the indexes so agents can find it."
diataxis: how-to
status: stable
last_reviewed: 2026-09-20
tags: [docs, diataxis, frontmatter, llms-txt]
related:
  - ../reference/frontmatter-schema.md
---
```

## Files exempt from the schema

| Path              | Reason                                                        |
| ----------------- | ------------------------------------------------------------- |
| `docs/llms.txt`   | Follows the llms.txt format, not Markdown with frontmatter.  |
| `docs/_templates/*.md` | Carry placeholder frontmatter by design; `_templates/README.md` is a normal index page. |
