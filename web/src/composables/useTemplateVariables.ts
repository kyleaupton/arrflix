import { computed } from 'vue'
import { useQuery } from '@tanstack/vue-query'
import { policiesGetFieldsOptions } from '@/client/@tanstack/vue-query.gen'
import type { FieldDefinition } from '@/client/types.gen'

export interface TemplateVariable {
  /** Template syntax path, e.g., ".Title", ".Quality.Resolution" */
  path: string
  /** Human-readable label */
  label: string
  /** Namespace group: Media, Quality, Candidate, MediaInfo */
  namespace: string
  /** Value type for display hints */
  valueType: string
  /** Whether this is only available post-download */
  postDownloadOnly: boolean
}

export interface TemplateFunction {
  name: string
  label: string
  description: string
}

/** Available template functions */
export const TEMPLATE_FUNCTIONS: TemplateFunction[] = [
  {
    name: 'clean',
    label: 'clean',
    description: 'Sanitizes value and treats "unknown" as empty',
  },
  {
    name: 'sanitize',
    label: 'sanitize',
    description: 'Removes path-unsafe characters',
  },
]

/**
 * Converts snake_case to PascalCase with Go acronym conventions
 * e.g., "clean_title" -> "CleanTitle", "tmdb_id" -> "TmdbID", "guid" -> "GUID"
 */
function snakeToPascal(str: string): string {
  // Special compound words that need specific capitalization
  const specialWords: Record<string, string> = {
    mediainfo: 'MediaInfo',
    tmdb: 'Tmdb',
  }

  // Common Go acronyms that should be all uppercase
  const acronyms = new Set(['id', 'guid', 'url', 'http', 'https', 'api', 'uri', 'uuid'])

  // Check if the entire string is a special word
  const lower = str.toLowerCase()
  if (specialWords[lower]) {
    return specialWords[lower]
  }

  return str
    .split('_')
    .map((part) => {
      const partLower = part.toLowerCase()
      // If the entire part is an acronym, uppercase it
      if (acronyms.has(partLower)) {
        return part.toUpperCase()
      }
      // Check for special words in parts
      if (specialWords[partLower]) {
        return specialWords[partLower]
      }
      // Otherwise, just capitalize first letter
      return part.charAt(0).toUpperCase() + part.slice(1)
    })
    .join('')
}

/**
 * Converts an API field path to Go template syntax
 * e.g., "media.clean_title" -> ".Media.CleanTitle"
 */
function toTemplatePath(apiPath: string): string {
  return (
    '.' +
    apiPath
      .split('.')
      .map((part) => snakeToPascal(part))
      .join('.')
  )
}

/**
 * Extracts the namespace from an API field path
 * e.g., "media.title" -> "Media", "mediainfo.video_codec" -> "MediaInfo"
 */
function extractNamespace(apiPath: string): string {
  const NAMESPACE_MAP: Record<string, string> = {
    candidate: 'Candidate',
    quality: 'Quality',
    media: 'Media',
    mediainfo: 'MediaInfo',
    encode: 'Encode',
  }

  const ns = apiPath.split('.')[0]
  if (!ns) return 'Unknown'
  return NAMESPACE_MAP[ns] || ns.charAt(0).toUpperCase() + ns.slice(1)
}

/**
 * Composable for accessing template variables from the API
 */
export function useTemplateVariables(options?: { mediaType?: 'movie' | 'series' }) {
  const { data: fields, isLoading, error } = useQuery(policiesGetFieldsOptions())

  /** All variables transformed for template use */
  const allVariables = computed<TemplateVariable[]>(() => {
    if (!fields.value) return []

    return fields.value.map((field: FieldDefinition) => ({
      path: toTemplatePath(field.path),
      label: field.label,
      namespace: extractNamespace(field.path),
      valueType: field.valueType || 'string',
      postDownloadOnly: field.path.startsWith('mediainfo.'),
    }))
  })

  /** Top-level shortcut variables - REMOVED in favor of namespaced fields */
  const shortcutVariables = computed<TemplateVariable[]>(() => {
    return []
  })

  /** Variables grouped by namespace */
  const variablesByNamespace = computed(() => {
    const groups: Record<string, TemplateVariable[]> = {}

    for (const variable of allVariables.value) {
      // Skip series-only fields for movie templates
      if (options?.mediaType === 'movie') {
        if (
          variable.path === '.Media.Season' ||
          variable.path === '.Media.Episode' ||
          variable.path === '.Media.EpisodeTitle'
        ) {
          continue
        }
      }

      if (!groups[variable.namespace]) {
        groups[variable.namespace] = []
      }
      groups[variable.namespace]!.push(variable)
    }

    return groups
  })

  /** Flat list of commonly used variables */
  const commonVariables = computed<TemplateVariable[]>(() => {
    // Common fields for quick access
    const commonPaths = [
      '.Media.Title',
      '.Media.CleanTitle',
      '.Media.Year',
      '.Quality.Resolution',
      '.Quality.Source',
      '.Quality.Full',
    ]

    return allVariables.value.filter((v) => commonPaths.includes(v.path))
  })

  /** Search variables by label or path */
  const searchVariables = (query: string): TemplateVariable[] => {
    const q = query.toLowerCase()

    return allVariables.value.filter(
      (v) => v.label.toLowerCase().includes(q) || v.path.toLowerCase().includes(q),
    )
  }

  /** Get variable by template path */
  const getVariableByPath = (path: string): TemplateVariable | undefined => {
    return allVariables.value.find((v) => v.path === path)
  }

  return {
    allVariables,
    shortcutVariables,
    variablesByNamespace,
    commonVariables,
    searchVariables,
    getVariableByPath,
    functions: TEMPLATE_FUNCTIONS,
    isLoading,
    error,
  }
}
