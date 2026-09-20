---
title: "Why Diátaxis"
description: "The reasoning behind the four-way split, and how frontmatter and llms.txt make that structure discoverable by agents."
diataxis: explanation
status: stable
last_reviewed: 2026-09-20
tags: [docs, diataxis, agents, design-decision]
related:
  - ../README.md
  - ../reference/frontmatter-schema.md
  - ../reference/llms-txt.md
  - ../how-to/add-a-document.md
---

# Why Diátaxis

## The problem with one kind of document

Most projects start with a single README that grows. It begins as a
quick-start, gains a table of configuration options, then a paragraph about why
a design was chosen, then a troubleshooting section. Each addition is
reasonable on its own. The result serves nobody well. The newcomer drowns in
options, the expert scrolls past the lesson, and the person asking "why" finds
the answer wedged between two command listings.

The underlying issue is that readers arrive with different needs, and a page
written for one need is actively unhelpful for another. A lesson that stops to
explain rationale loses the learner. A reference table that tries to teach
loses its precision.

## Two axes, four kinds

[Diátaxis](https://diataxis.fr), by Daniele Procida, observes that a reader's
need can be located on two axes:

- **Action versus cognition.** Is the reader trying to do something, or trying
  to know something?
- **Acquisition versus application.** Is the reader studying the subject, or
  already working with it?

The axes cross to give four kinds of documentation, each with a distinct job.

|               | Acquisition (learning)    | Application (working)     |
| ------------- | ------------------------- | ------------------------- |
| **Action**    | Tutorial: a lesson        | How-to guide: a recipe    |
| **Cognition** | Explanation: a discussion | Reference: a description  |

The value is not the taxonomy but the discipline it imposes. When every page
has exactly one job, the author knows what to leave out, the reader knows what
to expect, and gaps become visible. An empty `tutorials/` directory is a to-do
item rather than an invisible absence.

## Why this project adopted it from the first commit

localclaw is new and expects a large share of its contributions to come from
coding agents. Two properties of that situation favour deciding the structure
early.

First, agents follow conventions well when the convention is explicit and
poorly when it is implied. A layout with four named sections and a written
two-question test for choosing between them removes a judgement call that
would otherwise be made differently each time.

Second, retrofitting a structure onto existing pages means splitting and
rewriting them. Adopting it while the tree is empty costs nothing.

## Making the structure discoverable

Diátaxis solves the authoring problem. Two additions solve the finding problem
for agents.

**Frontmatter** makes each page self-describing. A tool, or an agent reading
the first ten lines, learns the page's title, one-line summary, Diátaxis type,
lifecycle status and review date without parsing the body. The `diataxis`
field deliberately repeats information already present in the path. A page
that is moved or copied keeps its classification, and a mismatch between field
and directory becomes a detectable error rather than a silent one.

**llms.txt** provides a single curated map. The
[llms.txt](https://llmstxt.org) format was designed for exactly this case: a
short, structured index that a language model can load whole and use to decide
which pages to read in full. Requiring each entry to reuse the page's
frontmatter title and description verbatim keeps the index honest, and the
how-to for adding a page includes a check for it.

The three layers answer three different questions. `AGENTS.md` answers "what
are the rules?" `llms.txt` answers "what pages exist and which should I read?"
Frontmatter answers "what is this page I am looking at?"

## What lives outside `docs/`

`AGENTS.md` at the repository root is read automatically by coding agents and
is deliberately kept short. It is closest in spirit to a how-to guide, but it
is an instruction file for tooling rather than documentation for readers, so it
stays where tooling expects it. Anything in it longer than a few lines should
move into `docs/how-to/` and be linked.

Code comments and docstrings document code in place. They are not reference
documentation. Reference lives in `docs/reference/`, where it can be found
without reading source.

## Trade-offs

The cost of Diátaxis is more, smaller files and the discipline to split a
draft that wants to be two things. Cross-linking a how-to guide with the
explanation it relies on takes deliberate effort. The section indexes and
`llms.txt` are two lists to keep in step. The how-to makes registration a
required step so that drift is caught in review rather than accumulating.

Alternatives considered were a single README, rejected for the reasons above;
a wiki, rejected because it sits outside the pull request flow that
`AGENTS.md` requires; and an ad hoc `docs/` folder with topic-named
subdirectories, rejected because a topic name does not tell a reader what kind
of page to expect.
