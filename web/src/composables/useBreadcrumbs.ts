import { computed } from 'vue'
import { useRoute } from 'vue-router'

export interface BreadcrumbItem {
  label: string
  path: string
  isLast: boolean
}

const ROUTES_WITH_BREADCRUMBS = ['/settings']

const SPECIAL_CASES: Record<string, string> = {
  'name-templates': 'Name Templates',
}

function formatBreadcrumbLabel(segment: string): string {
  if (SPECIAL_CASES[segment]) {
    return SPECIAL_CASES[segment]
  }

  // Default: capitalize each word
  return segment
    .split('-')
    .map((word) => word.charAt(0).toUpperCase() + word.slice(1))
    .join(' ')
}

export function useBreadcrumbs() {
  const route = useRoute()

  const shouldShowBreadcrumbs = computed(() => {
    return ROUTES_WITH_BREADCRUMBS.some((path) => route.path.startsWith(path))
  })

  const breadcrumbItems = computed<BreadcrumbItem[]>(() => {
    if (!shouldShowBreadcrumbs.value) return []

    const pathSegments = route.path.split('/').filter(Boolean)
    const items: BreadcrumbItem[] = []

    let currentPath = ''
    pathSegments.forEach((segment, index) => {
      currentPath += `/${segment}`
      const isLast = index === pathSegments.length - 1

      items.push({
        label: formatBreadcrumbLabel(segment),
        path: currentPath,
        isLast,
      })
    })

    return items
  })

  return {
    shouldShowBreadcrumbs,
    breadcrumbItems,
  }
}
