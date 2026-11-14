# Routing Rules

Routing rules decide where each grab goes: which downloader runs it, which library receives it, and which name template names it. You manage them under **Settings ▸ Routing**.

## Do I Need Routing Rules?

No. If you set a default downloader, library, and name template, Arrflix uses those for everything. Rules are useful when you want different behavior based on the content, like routing 4K releases to a dedicated library.

## How Rules Work

Each rule has two parts:

1. **Conditions** - What the release or title must look like for the rule to match.
2. **Actions** - Which downloader, library, and name template to use when it does. Any of the three can be left unset.

Rules form a single ordered list that you rearrange with the up and down arrows. They evaluate **top to bottom, and the first match wins**. A disabled rule is skipped.

### Example

> **Rule: "4K to dedicated library"**
> - Condition: `Resolution` equals `2160p`
> - Actions: Library → "4K Movies", Name Template → "TRaSH Recommended"

When a 2160p release is grabbed, this rule matches and routes the file to your 4K library with detailed naming. The downloader isn't set by the rule, so the default downloader is used. A 1080p release skips this rule and falls through to the next one, or to your defaults.

## Building Conditions

A condition compares a **field** with a **value** using an **operator**. The value input adapts to the field: some fields offer a dropdown of options (like resolution), others take free-form input (like a minimum seeder count).

### Available Fields

Fields are grouped by namespace. The main ones:

- **Candidate.** The release as the indexer reports it: title, size, indexer, protocol, seeders, peers, age, grabs.
- **Quality.** Parsed quality: resolution, source, repack and version.
- **Identity** and **Encode.** What the release name says it is: title, year, season and episode, edition, release group.
- **Media.** The movie or series: type, title, year, TMDB ID, season, episode.
- **Want.** Why the grab is happening: trigger, requesters, and quality tier.

### Operators

| Field Type | Operators |
|------------|-----------|
| Number | `==`, `!=`, `>`, `>=`, `<`, `<=` |
| Text | `==`, `!=`, `contains`, `in`, `not in` |
| Enum | `==`, `!=`, `in`, `not in` |
| Boolean | `==`, `!=` |

## Continue Evaluating

By default a matching rule stops evaluation. Turn on **Continue evaluating after this rule matches** to let later rules contribute too. Later matches only fill slots that are still unset, so an earlier rule always wins a conflict. This lets one rule pick the library while another picks the name template.

## Evaluation Order

1. Rules are checked in list order, skipping disabled ones.
2. The first matching rule applies its actions. Evaluation stops there unless the rule has **Continue** turned on.
3. Any downloader, library, or name template still unset falls back to your defaults.

If no rule and no default supplies a downloader, library, or name template, the grab fails with an error.

The **Test rules** panel on the Routing page dry-runs your rules against a release title and shows which rules fired.
