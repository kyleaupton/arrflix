import { useQuery } from '@tanstack/vue-query'
import { routingGetFieldsOptions } from '@/client/@tanstack/vue-query.gen'
import type { FieldDefinition } from '@/client/types.gen'

export function useRoutingFields() {
  const { data: fields, isLoading, error } = useQuery(routingGetFieldsOptions())

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
    isLoading,
    error,
    getFieldByPath,
    getValidOperators,
  }
}
