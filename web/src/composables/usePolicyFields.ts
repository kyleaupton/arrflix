import { computed } from 'vue'
import { useQuery } from '@tanstack/vue-query'
import { policiesGetFieldsOptions } from '@/client/@tanstack/vue-query.gen'
import type { FieldDefinition } from '@/client/types.gen'

export function usePolicyFields() {
  const { data: fields, isLoading, error } = useQuery(policiesGetFieldsOptions())

  // Group fields by category (candidate vs quality)
  const candidateFields = computed(() => {
    return fields.value?.filter((f) => f.path.startsWith('candidate.')) || []
  })

  const qualityFields = computed(() => {
    return fields.value?.filter((f) => f.path.startsWith('quality.')) || []
  })

  // Get field definition by path
  const getFieldByPath = (path: string): FieldDefinition | undefined => {
    return fields.value?.find((f) => f.path === path)
  }

  // Get valid operators for a field
  const getValidOperators = (field: FieldDefinition | undefined) => {
    if (!field) return []
    return field.operators || []
  }

  return {
    fields,
    candidateFields,
    qualityFields,
    isLoading,
    error,
    getFieldByPath,
    getValidOperators,
  }
}

