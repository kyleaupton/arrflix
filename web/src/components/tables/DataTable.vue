<!-- eslint-disable @typescript-eslint/no-explicit-any -->
<script setup lang="ts" generic="T extends Record<string, any>">
import { computed, ref, onMounted, watch, h, defineComponent, type PropType } from 'vue'
import { useQuery } from '@tanstack/vue-query'
import {
  useVueTable,
  getCoreRowModel,
  getFilteredRowModel,
  getPaginationRowModel,
  getSortedRowModel,
  type ColumnDef,
  type SortingState,
  type ColumnFiltersState,
} from '@tanstack/vue-table'
import { valueUpdater } from '@/components/ui/table/utils'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
  DropdownMenuSeparator,
} from '@/components/ui/dropdown-menu'
import { MoreVertical, Search, ChevronLeft, ChevronRight } from 'lucide-vue-next'
import { Empty, EmptyContent, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { cn } from '@/lib/utils'
import type { TableColumn, TableAction } from './types'

// Re-export types for convenience
export type { TableColumn, TableAction }

// Component to render table cells
const RenderCell = defineComponent({
  props: {
    cell: {
      type: Object as PropType<any>,
      required: true,
    },
    row: {
      type: Object as PropType<any>,
      required: true,
    },
    table: {
      type: Object as PropType<any>,
      required: true,
    },
  },
  setup(props) {
    return () => {
      const cellDef = props.cell.column.columnDef
      if (typeof cellDef.cell === 'function') {
        const result = (cellDef.cell as any)({
          cell: props.cell,
          column: props.cell.column,
          row: props.row,
          table: props.table,
        })
        // If it's a VNode, return it directly
        if (result && typeof result === 'object' && 'type' in result) {
          return result
        }
        // If it's HTML string, render it
        if (typeof result === 'string' && result.includes('<')) {
          return h('div', { innerHTML: result })
        }
        return result
      }
      return props.cell.getValue()
    }
  },
})

interface Props {
  data?: T[]
  columns: TableColumn<T>[]
  actions?: TableAction<T>[]
  loading?: boolean
  emptyMessage?: string
  selectionMode?: 'single' | 'multiple' | 'checkbox' | 'radiobutton'
  selectable?: boolean
  paginator?: boolean
  rows?: number
  scrollable?: boolean
  scrollHeight?: string
  virtualScrollerOptions?: any
  sortField?: string
  sortOrder?: 1 | -1
  filterable?: boolean
  searchable?: boolean
  searchPlaceholder?: string
  // Async loading support (legacy)
  asyncData?: () => Promise<T[]>
  autoLoad?: boolean
  // TanStack Query support - pass query options to use TanStack Query for data fetching
  // This takes precedence over asyncData when both are provided
  queryOptions?: any
}

const props = withDefaults(defineProps<Props>(), {
  data: () => [],
  loading: false,
  emptyMessage: 'No data available',
  selectionMode: 'single',
  selectable: false,
  paginator: true,
  rows: 10,
  sortField: '',
  sortOrder: 1,
  filterable: true,
  searchable: true,
  searchPlaceholder: 'Search...',
  autoLoad: true,
})

const emit = defineEmits<{
  selectionChange: [selection: T | T[] | null]
  rowSelect: [row: T]
  rowUnselect: [row: T]
  'data-loaded': [data: T[]]
  'load-error': [error: Error]
  'query-success': [data: T[]]
  'query-error': [error: Error]
}>()

// const selectedRows = ref<T[]>([])
const globalFilter = ref('')
const sorting = ref<SortingState>([])
const columnFilters = ref<ColumnFiltersState>([])

// Async loading state (legacy)
const legacyAsyncData = ref<T[]>([])
const asyncLoading = ref(false)
const asyncError = ref<Error | null>(null)

// TanStack Query integration
const queryResult = props.queryOptions ? useQuery(props.queryOptions) : null

// Determine which data source to use
// Priority: TanStack Query > asyncData > static data
const tableData = computed(() => {
  if (props.queryOptions && queryResult?.data?.value) {
    return Array.isArray(queryResult.data.value) ? queryResult.data.value : []
  }
  return props.asyncData ? legacyAsyncData.value : props.data || []
})

const isLoading = computed(() => {
  if (props.queryOptions && queryResult?.isLoading?.value) {
    return queryResult.isLoading.value
  }
  return props.asyncData ? asyncLoading.value : props.loading
})

const queryError = computed(() => {
  if (props.queryOptions && queryResult?.error?.value) {
    return queryResult.error.value
  }
  return asyncError.value
})

const getNestedValue = (obj: any, path: string) => {
  return path.split('.').reduce((current, key) => current?.[key], obj)
}

const getRowId = (row: T): string => {
  return (row as any).id?.toString() || JSON.stringify(row)
}

// Convert TableColumn to TanStack Table ColumnDef
const columnDefs = computed<ColumnDef<T>[]>(() => {
  const cols: ColumnDef<T>[] = []

  // Selection column
  if (props.selectable && props.selectionMode === 'multiple') {
    cols.push({
      id: 'select',
      header: ({ table }) =>
        h(Checkbox, {
          checked: table.getIsAllPageRowsSelected(),
          onCheckedChange: (value: boolean) => table.toggleAllPageRowsSelected(!!value),
          'aria-label': 'Select all',
        }),
      cell: ({ row }) =>
        h(Checkbox, {
          checked: row.getIsSelected(),
          onCheckedChange: (value: boolean) => row.toggleSelected(!!value),
          'aria-label': 'Select row',
        }),
      enableSorting: false,
      enableHiding: false,
    })
  }

  // Data columns
  props.columns.forEach((col) => {
    cols.push({
      id: col.key as string,
      accessorKey: col.key as string,
      header: col.label,
      cell: ({ row }) => {
        const value = getNestedValue(row.original, col.key as string)
        if (col.render) {
          const rendered = col.render(value, row.original)
          // If render returns HTML string, render it as HTML
          if (typeof rendered === 'string' && rendered.includes('<')) {
            return h('div', { innerHTML: rendered })
          }
          return rendered
        }
        return value
      },
      enableSorting: col.sortable ?? false,
      enableColumnFilter: col.filterable ?? false,
      size: col.width ? parseInt(col.width) : undefined,
      meta: {
        align: (col.align || 'left') as 'left' | 'center' | 'right',
      },
    })
  })

  // Actions column
  if (props.actions && props.actions.length > 0) {
    cols.push({
      id: 'actions',
      header: '',
      cell: ({ row }) => {
        const visibleActions = props.actions!.filter((action) =>
          action.visible ? action.visible(row.original) : true,
        )
        if (visibleActions.length === 0) return null

        return h(DropdownMenu, () => [
          h(
            DropdownMenuTrigger,
            { asChild: true },
            {
              default: () =>
                h(
                  Button,
                  { variant: 'ghost', class: 'h-8 w-8 p-0' },
                  {
                    default: () => [
                      h('span', { class: 'sr-only' }, 'Open menu'),
                      h(MoreVertical, { class: 'h-4 w-4' }),
                    ],
                  },
                ),
            },
          ),
          h(
            DropdownMenuContent,
            { align: 'end' },
            {
              default: () =>
                visibleActions.map((action, idx) => {
                  const disabled = action.disabled ? action.disabled(row.original) : false
                  const isDestructive = action.severity === 'danger'
                  return [
                    idx > 0 ? h(DropdownMenuSeparator) : null,
                    h(
                      DropdownMenuItem,
                      {
                        disabled,
                        variant: isDestructive ? 'destructive' : 'default',
                        onClick: () => !disabled && action.command(row.original),
                      },
                      { default: () => action.label },
                    ),
                  ]
                }),
            },
          ),
        ])
      },
      enableSorting: false,
      enableHiding: false,
      size: 50,
    })
  }

  return cols
})

// Initialize sorting from props
watch(
  () => [props.sortField, props.sortOrder],
  () => {
    if (props.sortField) {
      sorting.value = [
        {
          id: props.sortField,
          desc: props.sortOrder === -1,
        },
      ]
    }
  },
  { immediate: true },
)

const table = useVueTable({
  get data() {
    return tableData.value
  },
  get columns() {
    return columnDefs.value
  },
  getCoreRowModel: getCoreRowModel(),
  getFilteredRowModel: getFilteredRowModel(),
  getSortedRowModel: getSortedRowModel(),
  getPaginationRowModel: props.paginator ? getPaginationRowModel() : undefined,
  onSortingChange: (updater) => valueUpdater(updater, sorting),
  onColumnFiltersChange: (updater) => valueUpdater(updater, columnFilters),
  onGlobalFilterChange: (updater) => valueUpdater(updater, globalFilter),
  getRowId: (row) => getRowId(row),
  enableRowSelection: props.selectable,
  state: {
    get sorting() {
      return sorting.value
    },
    get columnFilters() {
      return columnFilters.value
    },
    get globalFilter() {
      return globalFilter.value
    },
  },
  initialState: {
    pagination: {
      pageSize: props.rows,
    },
  },
  manualPagination: false,
  manualSorting: false,
  manualFiltering: false,
})

// Handle selection changes
watch(
  () => table.getSelectedRowModel().rows,
  (selectedRows) => {
    const selection = selectedRows.map((row) => row.original)
    if (props.selectionMode === 'single') {
      emit('selectionChange', selection.length > 0 ? selection[0] : null)
    } else {
      emit('selectionChange', selection.length > 0 ? selection : null)
    }
  },
  { deep: true },
)

// Async loading functions
const loadAsyncData = async () => {
  if (!props.asyncData) return

  try {
    asyncLoading.value = true
    asyncError.value = null
    const data = await props.asyncData()
    legacyAsyncData.value = data
    emit('data-loaded', data)
  } catch (error) {
    asyncError.value = error as Error
    emit('load-error', error as Error)
  } finally {
    asyncLoading.value = false
  }
}

// Expose load function for manual triggering
const loadData = () => {
  if (props.asyncData) {
    loadAsyncData()
  }
}

// Auto-load on mount if enabled
onMounted(() => {
  if (props.autoLoad && props.asyncData) {
    loadAsyncData()
  }
})

// Watch for asyncData changes
watch(
  () => props.asyncData,
  (newAsyncData) => {
    if (newAsyncData && props.autoLoad) {
      loadAsyncData()
    }
  },
)

// Watch for TanStack Query state changes
watch(
  () => queryResult?.data?.value,
  (newData) => {
    if (newData && props.queryOptions) {
      emit('query-success', Array.isArray(newData) ? newData : [])
    }
  },
)

watch(
  () => queryResult?.error?.value,
  (newError) => {
    if (newError && props.queryOptions) {
      emit('query-error', newError)
    }
  },
)

// Handle row click for single selection mode
const handleRowClick = (row: any) => {
  if (props.selectable && props.selectionMode === 'single') {
    // If clicking an already selected row, deselect it
    if (row.getIsSelected()) {
      row.toggleSelected(false)
    } else {
      // Deselect all rows first, then select this one
      table.resetRowSelection()
      row.toggleSelected(true)
    }
  }
}

// Expose methods for parent components
defineExpose({
  loadData,
  asyncData: legacyAsyncData.value,
  asyncLoading: asyncLoading.value,
  asyncError: asyncError.value,
  // TanStack Query methods
  refetch: queryResult?.refetch,
  queryResult: queryResult,
  queryError: queryError.value,
  table,
})
</script>

<template>
  <div class="data-table-container space-y-4">
    <!-- Search Bar -->
    <div v-if="searchable" class="relative max-w-sm">
      <Search class="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
      <Input v-model="globalFilter" :placeholder="searchPlaceholder" class="pl-8" />
    </div>

    <!-- Loading State -->
    <div v-if="isLoading" class="flex items-center justify-center py-8">
      <div class="text-muted-foreground">Loading...</div>
    </div>

    <!-- Data Table -->
    <div v-else>
      <!-- Empty State -->
      <Empty v-if="table.getRowModel().rows.length === 0">
        <EmptyContent>
          <EmptyMedia variant="icon">
            <slot name="empty-icon" />
          </EmptyMedia>
          <EmptyTitle>{{ queryError ? `Error: ${queryError}` : emptyMessage }}</EmptyTitle>
        </EmptyContent>
      </Empty>

      <!-- Table -->
      <div v-else class="rounded-md border">
        <Table>
          <TableHeader>
            <TableRow v-for="headerGroup in table.getHeaderGroups()" :key="headerGroup.id">
              <TableHead
                v-for="header in headerGroup.headers"
                :key="header.id"
                :class="
                  cn(
                    (header.column.columnDef.meta as any)?.align === 'center' && 'text-center',
                    (header.column.columnDef.meta as any)?.align === 'right' && 'text-right',
                    header.column.id === 'actions' && 'w-[50px]',
                  )
                "
                :style="{
                  width: header.getSize() !== 150 ? `${header.getSize()}px` : undefined,
                }"
              >
                <div
                  v-if="!header.isPlaceholder"
                  :class="
                    cn(
                      'flex items-center gap-2',
                      header.column.getCanSort() && 'cursor-pointer select-none',
                    )
                  "
                  @click="header.column.getToggleSortingHandler()?.($event)"
                >
                  <span>
                    {{
                      typeof header.column.columnDef.header === 'function'
                        ? (header.column.columnDef.header as any)({
                            column: header.column,
                            header,
                            table,
                          })
                        : header.column.columnDef.header
                    }}
                  </span>
                  <span v-if="header.column.getCanSort()" class="inline-flex items-center">
                    {{
                      {
                        asc: '↑',
                        desc: '↓',
                      }[header.column.getIsSorted() as string] ?? '⇅'
                    }}
                  </span>
                </div>
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            <TableRow
              v-for="row in table.getRowModel().rows"
              :key="row.id"
              :data-state="row.getIsSelected() && 'selected'"
              :class="cn(selectable && 'cursor-pointer')"
              @click="selectable && selectionMode === 'single' && handleRowClick(row)"
            >
              <TableCell
                v-for="cell in row.getVisibleCells()"
                :key="cell.id"
                :class="
                  cn(
                    (cell.column.columnDef.meta as any)?.align === 'center' && 'text-center',
                    (cell.column.columnDef.meta as any)?.align === 'right' && 'text-right',
                  )
                "
              >
                <RenderCell :cell="cell" :row="row" :table="table" />
              </TableCell>
            </TableRow>
          </TableBody>
        </Table>
      </div>
    </div>

    <!-- Pagination -->
    <div
      v-if="paginator && table.getPageCount() > 1"
      class="flex items-center justify-between px-2"
    >
      <div class="flex-1 text-sm text-muted-foreground">
        {{ table.getFilteredSelectedRowModel().rows.length }} of
        {{ table.getFilteredRowModel().rows.length }} row(s) selected.
      </div>
      <div class="flex items-center space-x-6 lg:space-x-8">
        <div class="flex items-center space-x-2">
          <p class="text-sm font-medium">Rows per page</p>
          <select
            :value="table.getState().pagination.pageSize"
            @change="table.setPageSize(Number(($event.target as HTMLSelectElement).value))"
            class="h-8 w-[70px] rounded-md border border-input bg-background px-2 text-sm"
          >
            <option :value="10">10</option>
            <option :value="20">20</option>
            <option :value="30">30</option>
            <option :value="50">50</option>
            <option :value="100">100</option>
          </select>
        </div>
        <div class="flex w-[100px] items-center justify-center text-sm font-medium">
          Page {{ table.getState().pagination.pageIndex + 1 }} of
          {{ table.getPageCount() }}
        </div>
        <div class="flex items-center space-x-2">
          <Button
            variant="outline"
            class="h-8 w-8 p-0"
            :disabled="!table.getCanPreviousPage()"
            @click="table.previousPage()"
          >
            <span class="sr-only">Go to previous page</span>
            <ChevronLeft class="h-4 w-4" />
          </Button>
          <Button
            variant="outline"
            class="h-8 w-8 p-0"
            :disabled="!table.getCanNextPage()"
            @click="table.nextPage()"
          >
            <span class="sr-only">Go to next page</span>
            <ChevronRight class="h-4 w-4" />
          </Button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.data-table-container {
  width: 100%;
}
</style>
