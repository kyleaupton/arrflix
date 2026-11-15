---
paths:
  - "web/src/**"
---

# Looking at the UI you build

Two MCP servers give you a real browser on the running app, at `http://localhost:8484`:

| Server | Browser | Size |
| --- | --- | --- |
| `browser-desktop` | Chromium | 1440×900 |
| `browser-mobile` | WebKit, emulating an iPhone 15 | 393×659, touch |

Both start signed in as the seeded admin. The mobile entry matters: Arrflix is used as an iOS PWA, and WebKit is the engine that shows Safari's layout behavior.

**Before calling a visible UI change done, open each screen you changed in both browsers and look at it.** Navigate to the screen, take a screenshot, and check what you meant to change is there and nothing near it broke. Use `browser_snapshot` (the accessibility tree) to check labels, roles and what's present; use `browser_take_screenshot` to check how it looks. Fix what you find before finishing.

The browser runs inside the dev container, so file paths in its replies (the `[Snapshot](…)` link after a navigation, saved screenshots) are container paths you can't open. Call `browser_snapshot` for the tree; screenshots come back as images in the reply.

- **Other perspective.** To see the requester view, log out and sign in as `user@example.com` / `password`. The admin is `admin@example.com` / `password`. Seeded users come from `just db-reseed`.
- **Fresh code.** The service worker is blocked, so a reload shows the current Vite output. Vite hot-reloads on save, so reload after editing.
- **Not covered.** Safe-area insets, the iOS swipe-back gesture, push notifications and the PWA update prompt don't reproduce here. Say so rather than claiming you checked them.
- **Not a test tool.** Don't write Playwright tests or add browser checks to CI. Requirements are pinned by component tests (see `workflow.md`).

If the browser starts signed out, its log says why: usually the dev stack isn't up (`just up`) or the database isn't seeded (`just db-reseed`).
