<script setup lang="ts">
import { useQuery, useMutation } from '@tanstack/vue-query'
import { Plus, FileText } from 'lucide-vue-next'
import { toast } from 'vue-sonner'
import {
  getV1NameTemplatesOptions,
  deleteV1NameTemplatesByIdMutation,
} from '@/client/@tanstack/vue-query.gen'
import { type HandlersNameTemplateSwagger } from '@/client/types.gen'
import DataTable from '@/components/tables/DataTable.vue'
import {
  nameTemplateColumns,
  createNameTemplateActions,
} from '@/components/tables/configs/nameTemplateTableConfig'
import { useModal } from '@/composables/useModal'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import NameTemplateDialog from '@/components/modals/NameTemplateDialog.vue'

const modal = useModal()

// Data queries
const { data: templates, isLoading, refetch } = useQuery(getV1NameTemplatesOptions())

// Mutations
const deleteTemplateMutation = useMutation(deleteV1NameTemplatesByIdMutation())

// Handlers
const handleAddTemplate = () => {
  modal.open(NameTemplateDialog, {
    props: {
      template: null,
    },
    onClose: () => {
      refetch()
    },
  })
}

const handleEditTemplate = (template: HandlersNameTemplateSwagger) => {
  modal.open(NameTemplateDialog, {
    props: {
      template,
    },
    onClose: () => {
      refetch()
    },
  })
}

const handleDeleteTemplate = async (template: HandlersNameTemplateSwagger) => {
  if (!template.id) return
  const confirmed = await modal.confirm({
    title: 'Delete Template',
    message: `Are you sure you want to delete "${template.name}"?`,
    severity: 'danger',
  })
  if (!confirmed) return
  try {
    await deleteTemplateMutation.mutateAsync({ path: { id: template.id } })
    toast.success('Template deleted successfully')
    refetch()
  } catch (err) {
    const error = err as { message?: string }
    toast.error(error.message || 'Failed to delete template')
  }
}

const templateActions = createNameTemplateActions(handleEditTemplate, handleDeleteTemplate)
</script>

<template>
  <div class="flex flex-col gap-6">
    <Card>
      <CardHeader>
        <div class="flex items-center justify-between">
          <div>
            <CardTitle class="text-xl font-semibold mb-2">Name Templates</CardTitle>
            <p class="text-sm text-muted-foreground">
              Configure templates for naming downloaded media files.
            </p>
          </div>
          <Button @click="handleAddTemplate">
            <Plus class="mr-2 size-4" />
            Add Template
          </Button>
        </div>
      </CardHeader>
      <CardContent>
        <div v-if="isLoading" class="space-y-3">
          <Skeleton class="h-12 w-full" />
          <Skeleton class="h-12 w-full" />
          <Skeleton class="h-12 w-full" />
        </div>
        <DataTable
          v-else
          :data="templates || []"
          :columns="nameTemplateColumns"
          :actions="templateActions"
          :loading="isLoading"
          empty-message="No name templates configured"
          searchable
          search-placeholder="Search templates..."
          paginator
          :rows="10"
        >
          <template #empty-icon>
            <FileText class="size-5" />
          </template>
        </DataTable>
      </CardContent>
    </Card>
  </div>
</template>
