# Code comments

Comments are written for someone reading the file fresh in two years who never saw the PR that produced it. The single test for any comment:

> Does this still make sense with no knowledge of the refactor that created it?

If a comment only parses as a diff annotation against a previous state, it's process residue — cut it or rewrite it in the timeless present. Write **"X is parsed from Y"**, never **"X is _now_ parsed from Y (it used to be Z)"**.

**Two modes.** Scaffolding comments (phase notes, "this isn't wired up yet", reviewer reasoning-out-loud) are fine _while actively developing_ — they lower review cost in-flight. The discipline is to **strip scaffolding in a dedicated pass before a PR is ready to merge.** The failure mode isn't writing them; it's forgetting to remove them.

**Density should track surprise, not complexity.** Code you can't infer intent from earns heavy comments; self-evident code stays near-silent. Concretely:

- **Keep:** _why_ over _what_ — non-obvious rationale, trade-offs, and landmines ("looks wrong, but upstream does X"). Verbatim-port provenance (e.g. `// Parser.cs:67 — …` and "ported verbatim from submodules/…") is the highest-value comment we have: it's how we track upstream drift and the only thing that explains a gnarly ported regex. Contracts/invariants the type can't state ("pure, total — unparseable input yields zero-confidence fields, never an error"). Standard exported-symbol doc comments (Go convention; feeds godoc).
- **Cut:** changelogs and phase narration in source ("Phase 5 rebuild…", "brought X 92% → 100%") — that lives in git/PR. Comparisons to deleted code ("the old parser…", "no longer consumed"). Status that duplicates a test's enforced/reported flags. Reviewer deliberation — keep the _conclusion_, drop the working-out.
- **Trim:** restatement (`// Remove file extension` above `removeFileExtension(...)`) and editorializing ("the crown jewel").
