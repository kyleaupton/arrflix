<script setup lang="ts">
import { computed } from 'vue'
import { Badge } from '@/components/ui/badge'

interface Props {
  /** The template string to display */
  template: string
  /** For series: array of [show, season, episode] templates */
  seriesTemplates?: [string, string, string]
  /** Whether this is a series template */
  isSeries?: boolean
}

const props = withDefaults(defineProps<Props>(), {
  isSeries: false,
})

interface ParsedSegment {
  type: 'text' | 'variable'
  value: string
  func?: string
  optional?: boolean
  prefix?: string
  suffix?: string
}

// Matches {{if [func] .Field}}prefix{{[func] .Field}}suffix{{end}}
const OPTIONAL_VAR_REGEX =
  /\{\{if\s+(?:(clean|sanitize)\s+)?([.\w]+)\}\}(.*?)\{\{(?:(clean|sanitize)\s+)?\2\}\}(.*?)\{\{end\}\}/gs

/**
 * Parse a segment of text for simple variables (no conditionals)
 */
function parseSimpleVars(text: string): ParsedSegment[] {
  const segments: ParsedSegment[] = []
  const regex = /\{\{(\w+\s+)?(\.[^}]+)\}\}/g
  let lastIndex = 0
  let match

  while ((match = regex.exec(text)) !== null) {
    if (match.index > lastIndex) {
      segments.push({
        type: 'text',
        value: text.slice(lastIndex, match.index),
      })
    }

    const func = match[1]?.trim()
    segments.push({
      type: 'variable',
      value: match[2] || '',
      func: func || undefined,
    })

    lastIndex = regex.lastIndex
  }

  if (lastIndex < text.length) {
    segments.push({
      type: 'text',
      value: text.slice(lastIndex),
    })
  }

  return segments
}

/**
 * Parse a template string into segments of text and variables
 */
function parseTemplate(template: string): ParsedSegment[] {
  const segments: ParsedSegment[] = []
  let lastIndex = 0

  OPTIONAL_VAR_REGEX.lastIndex = 0
  let match

  while ((match = OPTIONAL_VAR_REGEX.exec(template)) !== null) {
    // Add content before the match
    if (match.index > lastIndex) {
      segments.push(...parseSimpleVars(template.slice(lastIndex, match.index)))
    }

    // Add optional variable
    segments.push({
      type: 'variable',
      value: match[2] || '',
      func: match[4] || undefined,
      optional: true,
      prefix: match[3] || '',
      suffix: match[5] || '',
    })

    lastIndex = match.index + match[0].length
  }

  // Add remaining content
  if (lastIndex < template.length) {
    segments.push(...parseSimpleVars(template.slice(lastIndex)))
  }

  return segments
}

/**
 * Format a variable for display: { Title } or { func Title }
 */
function formatVariable(segment: ParsedSegment): string {
  const displayValue = segment.value.startsWith('.') ? segment.value.slice(1) : segment.value
  if (segment.func) {
    return `${segment.func} ${displayValue}`
  }
  return displayValue
}

const parsedTemplate = computed(() => parseTemplate(props.template))

const parsedSeriesTemplates = computed(() => {
  if (!props.seriesTemplates) return null
  return props.seriesTemplates.map((t) => parseTemplate(t))
})
</script>

<template>
  <div class="inline-flex items-center flex-wrap gap-0.5 font-mono text-sm">
    <!-- Series: show all three templates on one line with / separators -->
    <template v-if="isSeries && parsedSeriesTemplates">
      <!-- Show template -->
      <template v-for="(segment, idx) in parsedSeriesTemplates[0]" :key="`show-${idx}`">
        <Badge
          v-if="segment.type === 'variable'"
          variant="default"
          class="font-mono text-xs"
          :class="segment.optional ? 'border border-dashed border-primary-foreground/40' : ''"
        >
          <span v-if="segment.optional && segment.prefix" class="opacity-50">{{
            segment.prefix
          }}</span>
          {{ formatVariable(segment) }}
          <span v-if="segment.optional && segment.suffix" class="opacity-50">{{
            segment.suffix
          }}</span>
        </Badge>
        <span v-else class="whitespace-pre">{{ segment.value }}</span>
      </template>
      <span class="mx-1 text-muted-foreground">/</span>

      <!-- Season template -->
      <template v-for="(segment, idx) in parsedSeriesTemplates[1]" :key="`season-${idx}`">
        <Badge
          v-if="segment.type === 'variable'"
          variant="default"
          class="font-mono text-xs"
          :class="segment.optional ? 'border border-dashed border-primary-foreground/40' : ''"
        >
          <span v-if="segment.optional && segment.prefix" class="opacity-50">{{
            segment.prefix
          }}</span>
          {{ formatVariable(segment) }}
          <span v-if="segment.optional && segment.suffix" class="opacity-50">{{
            segment.suffix
          }}</span>
        </Badge>
        <span v-else class="whitespace-pre">{{ segment.value }}</span>
      </template>
      <span class="mx-1 text-muted-foreground">/</span>

      <!-- Episode template -->
      <template v-for="(segment, idx) in parsedSeriesTemplates[2]" :key="`episode-${idx}`">
        <Badge
          v-if="segment.type === 'variable'"
          variant="default"
          class="font-mono text-xs"
          :class="segment.optional ? 'border border-dashed border-primary-foreground/40' : ''"
        >
          <span v-if="segment.optional && segment.prefix" class="opacity-50">{{
            segment.prefix
          }}</span>
          {{ formatVariable(segment) }}
          <span v-if="segment.optional && segment.suffix" class="opacity-50">{{
            segment.suffix
          }}</span>
        </Badge>
        <span v-else class="whitespace-pre">{{ segment.value }}</span>
      </template>
    </template>

    <!-- Single template (movies or fallback) -->
    <template v-else>
      <template v-for="(segment, idx) in parsedTemplate" :key="idx">
        <Badge
          v-if="segment.type === 'variable'"
          variant="default"
          class="font-mono text-xs"
          :class="segment.optional ? 'border border-dashed border-primary-foreground/40' : ''"
        >
          <span v-if="segment.optional && segment.prefix" class="opacity-50">{{
            segment.prefix
          }}</span>
          {{ formatVariable(segment) }}
          <span v-if="segment.optional && segment.suffix" class="opacity-50">{{
            segment.suffix
          }}</span>
        </Badge>
        <span v-else class="whitespace-pre">{{ segment.value }}</span>
      </template>
    </template>
  </div>
</template>
