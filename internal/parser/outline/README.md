# Outline scanner

`Scan` visits Markdown/MDX body lines with `bytes.Lines` and records ATX/setext
headings at their original 1-based positions. Fences close only with the same
marker and at least the opening length, followed by whitespace. Four-space and
tab-indented code is excluded. MDX import/export filtering preserves source
positions. Script/style/pre bodies and HTML comments do not become descriptions.

Goldmark handles formatted heading/first-paragraph text only when needed. Its
cumulative input budget, including the context prefix, is 256 KiB per file.
Beyond that, normalized source text is retained without AST parsing. No complete
Markdown document AST is built. YAML/TOML frontmatter is scanned for a matching
closing delimiter and decoded only up to 64 KiB; larger or invalid metadata is
ignored while the body remains available. These budgets bound decoder work.
Gomarkdown is no longer a dependency.

Existing description composition is preserved: frontmatter title/description,
then first H1 plus its first paragraph, then the first nonempty paragraph.
Inline code omission remains compatible with the old text extractor.

Fixture goldens retain the v0.3.0 values in `../testdata/outline/legacy.json`.
`corrections.json` records complete before/after results and explanations for the
four deliberate fixture changes: matched fences, original MDX lines (two files),
and TOML metadata. No existing assertion is removed.

The 3,651-file pinned corpus comparison has 3,568 identical outputs and 83
individually reviewed differences. Every differing file has old/new result
hashes and reasons in `testdata/bench/outline-differences.json` at the repo root.
Reasons cover MDX positions, legacy fence/indent position recovery, real headings
formerly swallowed after quoted fences/list/comment blocks, separating whitespace
at line breaks, root YAML fields instead of nested ones, and callout paragraphs.
The verifier rejects new, changed, unexplained or stale exceptions. See the
benchmark README for reproducible capture/comparison commands.
