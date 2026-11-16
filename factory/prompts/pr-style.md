<!--
How the factory writes for the human reviewer. run-stage puts the part after
the marker into any prompt with a {{PR_STYLE}} placeholder; the judge uses it
for the fields that open the PR body (needs_you, what_works).

Adapted from i-have-adhd (https://github.com/ayghri/i-have-adhd, MIT,
copyright its authors): the rules that fit a PR body, minus the ones about
holding a conversation.
-->
<!-- prompt -->
The reader is a maintainer reviewing many PRs. Anything below the first screen is easily missed, so the top of the PR has to tell them what to do and what changed. Write for someone who will act on the first lines and skim the rest.

- **Lead with the action.** Each `needs_you` item starts with the verb the reader performs: *Decide*, *Check*, *Confirm*, *Try*. Name the thing and where it is: "Decide whether disabling a user should write an audit entry; `UsersService.Update` doesn't." Never "You may want to consider…".
- **Only what needs them.** `needs_you` holds decisions and checks the reviewer must make before merging. A fact that's merely worth knowing is a finding, not a `needs_you` item. If nothing needs them, return an empty list. Don't invent work to fill it.
- **Make the wins concrete.** Each `what_works` item is a behavior a user or operator now sees, in plain terms: "A disabled user's refresh gets 401, and that session is revoked." Not "Updated the session service".
- **Five at most.** Group related items, most important first.
- **One idea per item.** No "and also", no sidebars, no "by the way".
- **Plain tone.** State a cause and a consequence; no "unfortunately", no "uh oh", no hedging adverb that adds nothing. Keep a hedge that carries real uncertainty.
- **No figures of speech.** Write the literal thing.
