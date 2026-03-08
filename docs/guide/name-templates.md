---
vue:
  template:
    compilerOptions:
      v-pre: true
---

<script setup>
</script>

<div v-pre>

# Name Templates

Name templates control how files are named and organized when imported into a library. They use Go template syntax (`{{ }}`) with access to metadata about the media, quality, and release.

## Template Types

Templates differ depending on the media type:

**Movies** have two template fields:
- **Directory template** — The folder name (e.g., `{{.Media.CleanTitle}} ({{.Media.Year}})`)
- **File template** — The filename (e.g., `{{.Media.CleanTitle}} ({{.Media.Year}}) [{{.Quality.Resolution}}]`)

**Series** have three template fields:
- **Show template** — The top-level folder (e.g., `{{.Media.Title}} ({{.Media.Year}})`)
- **Season template** — The season subfolder (e.g., `Season {{.Media.Season}}`)
- **File template** — The episode filename

File extensions are added automatically — you don't need to include them in your template.

## Available Variables

### Media

| Variable | Description | Example |
|----------|-------------|---------|
| `{{.Media.Title}}` | Original title | `Breaking Bad` |
| `{{.Media.CleanTitle}}` | Title with unsafe characters removed | `21 Jump Street` |
| `{{.Media.Year}}` | Release year | `2008` |
| `{{.Media.TmdbID}}` | TMDB identifier | `1396` |
| `{{.Media.Type}}` | Media type | `movie` or `series` |
| `{{.Media.Season}}` | Season number (zero-padded) | `01` |
| `{{.Media.Episode}}` | Episode number (zero-padded) | `05` |
| `{{.Media.EpisodeTitle}}` | Episode name | `Gray Matter` |

### Quality

| Variable | Description | Example |
|----------|-------------|---------|
| `{{.Quality.Full}}` | Complete quality string | `Bluray-2160p Remux` |
| `{{.Quality.Resolution}}` | Resolution | `1080p` |
| `{{.Quality.Source}}` | Source type | `BluRay`, `WEB-DL`, `HDTV` |

### Release

| Variable | Description | Example |
|----------|-------------|---------|
| `{{.Release.ReleaseGroup}}` | Release group | `GROUP` |
| `{{.Release.Edition}}` | Special edition | `Extended Cut` |

### MediaInfo (Post-Download)

These variables are only available after the file has been downloaded and analyzed:

| Variable | Description | Example |
|----------|-------------|---------|
| `{{.MediaInfo.VideoCodec}}` | Video codec | `H.265` |
| `{{.MediaInfo.VideoBitDepth}}` | Bit depth | `10` |
| `{{.MediaInfo.AudioCodec}}` | Audio codec | `TrueHD` |
| `{{.MediaInfo.AudioChannels}}` | Channel layout | `7.1` |
| `{{.MediaInfo.HDR}}` | HDR format | `HDR10`, `Dolby Vision` |
| `{{.MediaInfo.Container}}` | File container | `MKV` |

## Template Functions

Two functions are available for sanitizing values:

- **`clean`** — Removes filesystem-unsafe characters and returns empty string for "unknown" values. Use this for optional fields like episode titles: `{{clean .Media.EpisodeTitle}}`
- **`sanitize`** — Removes filesystem-unsafe characters but always returns a value.

## Conditional Sections

Use Go template `if` blocks to include text only when a value is present:

```
{{if .Release.Edition}} {edition-{{.Release.Edition}}}{{end}}
```

This outputs ` {edition-Extended Cut}` only when there's an edition value, and nothing otherwise.

## Presets

Arrflix includes two built-in presets to get you started:

### Simple

A clean, minimal naming scheme.

**Movie:**
```
Directory: {{.Media.CleanTitle}} ({{.Media.Year}})
File:      {{.Media.CleanTitle}} ({{.Media.Year}}) [{{.Quality.Resolution}}]
```

Result: `The Matrix (1999)/The Matrix (1999) [1080p].mkv`

**Series:**
```
Show:    {{.Media.Title}} ({{.Media.Year}})
Season:  Season {{.Media.Season}}
File:    {{.Media.Title}} - S{{.Media.Season}}E{{.Media.Episode}} - {{.Media.EpisodeTitle}} [{{.Quality.Resolution}}]
```

Result: `Breaking Bad (2008)/Season 01/Breaking Bad - S01E05 - Gray Matter [720p].mkv`

### TRaSH Recommended

A detailed scheme following [TRaSH Guides](https://trash-guides.info/) conventions, including codec, audio, and HDR information.

**Movie example:**
```
21 Jump Street (2012) {tmdb-9559}/
  21 Jump Street (2012) [Bluray-2160p Remux][TrueHD 7.1][HDR10][H.265]-GROUP.mkv
```

**Series example:**
```
Breaking Bad (2008)/Season 01/
  Breaking Bad - S01E05 - Gray Matter [Bluray-1080p][DTS 5.1][H.264]-GROUP.mkv
```

## Default Templates

Each media type (movie and series) can have one template marked as the **default**. When no [policy](./policy-engine) specifies a template, the default is used.

</div>
