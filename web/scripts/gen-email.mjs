// Compiles MJML email sources into the Go-embedded notification template tree.
//
// Source of truth: backend/internal/notifications/emailsrc/**/*.mjml. `partials/`
// holds shared mj-include fragments (head/header/footer), not standalone emails,
// so it is skipped. Each remaining source compiles to the mirrored path under
// backend/internal/notifications/templates/ with `.mjml` swapped for `.html.tmpl`
// — the exact file the Go renderer embeds and parses as an html/template.
//
// Go template actions ({{ ... }}) survive compilation: minify is off (so nothing
// rewrites the tokens) and templateSyntax marks {{ }} as opaque. mj-raw carries
// Go control flow (if/else/end) so it lands verbatim between compiled components.
//
// Run via `just gen-email` (folded into `just gen`). The compiled .html.tmpl files
// are generated — never hand-edit them; edit the .mjml source and recompile.

import { readdir, readFile, writeFile, mkdir } from 'node:fs/promises'
import { dirname, join, relative, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import mjml2html from 'mjml'

const here = dirname(fileURLToPath(import.meta.url))
const repoRoot = resolve(here, '../..')
const srcRoot = resolve(repoRoot, 'backend/internal/notifications/emailsrc')
const outRoot = resolve(repoRoot, 'backend/internal/notifications/templates')

async function* walk(dir) {
  for (const entry of await readdir(dir, { withFileTypes: true })) {
    const full = join(dir, entry.name)
    if (entry.isDirectory()) {
      yield* walk(full)
    } else if (entry.isFile() && entry.name.endsWith('.mjml')) {
      yield full
    }
  }
}

let compiled = 0
for await (const srcPath of walk(srcRoot)) {
  // partials/ are mj-include fragments, not compilable emails.
  if (relative(srcRoot, srcPath).split(/[\\/]/)[0] === 'partials') continue

  const source = await readFile(srcPath, 'utf8')

  let result
  try {
    result = await mjml2html(source, {
      // MJML 5 resolves mj-include paths against filePath's directory and confines
      // them to that tree. Pointing filePath at the source root makes every
      // include root-relative (`partials/head.mjml`) and depth-independent, so a
      // template's nesting depth never dictates its include paths.
      filePath: srcRoot,
      ignoreIncludes: false,
      validationLevel: 'soft', // collect issues rather than throw; fail below
      keepComments: false, // author notes in .mjml source must not ship in the email
      minify: false, // keep {{ }} tokens intact and the diff readable
      beautify: true,
      templateSyntax: [{ prefix: '{{', suffix: '}}' }],
    })
  } catch (err) {
    console.error(`MJML compile threw for ${relative(repoRoot, srcPath)}:\n  ${err.message}`)
    process.exit(1)
  }

  // MJML 5 seeds `errors` with a null per resolved mj-include; only non-null
  // entries are real validation failures.
  const errors = (result.errors ?? []).filter(Boolean)
  if (errors.length) {
    for (const e of errors) {
      console.error(`  ${relative(repoRoot, srcPath)}: ${e.formattedMessage ?? e.message}`)
    }
    console.error(`MJML validation failed for ${relative(repoRoot, srcPath)}`)
    process.exit(1)
  }

  const rel = relative(srcRoot, srcPath).replace(/\.mjml$/, '.html.tmpl')
  const outPath = join(outRoot, rel)
  // A Go-template comment marks provenance without shipping in the email —
  // html/template drops {{/* ... */}} at render time.
  const banner = `{{/* Code generated from ${relative(repoRoot, srcPath)} by \`just gen-email\`; DO NOT EDIT. */}}\n`

  await mkdir(dirname(outPath), { recursive: true })
  await writeFile(outPath, banner + result.html + '\n', 'utf8')

  console.log(`  ${relative(repoRoot, srcPath)} → ${relative(repoRoot, outPath)}`)
  compiled++
}

console.log(`Compiled ${compiled} email template(s).`)
