# Policy Engine

The Policy Engine automates decisions about how downloads are handled. It determines which downloader to use, which library to import into, and which name template to apply, based on rules you define.

## Do I Need Policies?

No. If you set default values for your downloader, library, and name template, Arrflix will use those for everything. Policies are useful when you want different behavior based on the content, like routing 4K releases to a dedicated library.

## How Policies Work

Each policy has two parts:

1. **A rule** (optional) - A condition that determines whether this policy applies.
2. **Actions** - What to do when the rule matches.

Policies are evaluated in **priority order** (highest first). The first policy whose rule matches has its actions applied. If a policy has no rule, it always matches.

The combined result of all matching actions is called a **plan**, the final set of instructions for how to handle the download.

### Example

> **Policy: "4K to dedicated library"**
> - Rule: `Resolution` equals `2160p`
> - Actions: Set Library → "4K Movies", Set Name Template → "TRaSH Recommended"

When you download a 2160p release, this policy matches and routes the file to your 4K library with detailed naming. A 1080p release would skip this policy and fall through to your defaults.

## Building Rules

Rules are built using three parts: a **field**, an **operator**, and a **value**.

The field and operator are selected from dropdowns. The value input adapts based on the field type. Some fields offer a dropdown of predefined options (like resolution), while others accept free-form input (like a minimum seeder count).

### Available Fields

Fields are organized by namespace:

**Candidate.** Information about the release itself:
`size`, `title`, `indexer`, `protocol`, `seeders`, `peers`, `age`, `grabs`

**Quality.** Parsed quality attributes:
`resolution`, `source`, `is_remux`, `is_repack`

**Release.** Release metadata:
`release_group`, `edition`

**Media.** Information about the movie or series:
`type`, `title`, `year`, `tmdb_id`, `season`, `episode`

### Operators

Available operators depend on the field type:

| Field Type | Operators |
|------------|-----------|
| Number | `==`, `!=`, `>`, `>=`, `<`, `<=` |
| Text | `==`, `!=`, `contains`, `in`, `not in` |
| Enum | `==`, `!=`, `in`, `not in` |
| Boolean | `==`, `!=` |

## Actions

Each policy can have one or more actions:

| Action | Effect |
|--------|--------|
| **Set Downloader** | Use a specific download client |
| **Set Library** | Import into a specific library |
| **Set Name Template** | Apply a specific naming template |
| **Stop Processing** | Skip all remaining policies |

Actions are applied in order. Multiple policies can contribute actions. For example, one policy might set the library while another sets the name template.

### Stop Processing

The **Stop Processing** action prevents any lower-priority policies from being evaluated. This is useful when you want to ensure that a catch-all policy doesn't override a more specific one.

## Evaluation Order

1. Policies are sorted by priority (highest first)
2. For each policy, the rule is evaluated against the current candidate
3. If the rule matches (or there is no rule), the policy's actions are applied
4. If a "Stop Processing" action is encountered, evaluation stops
5. After all policies are evaluated, any values not set by a policy fall back to defaults

If no policy sets a downloader, library, or name template, and no default is configured, the download will fail with an error.
