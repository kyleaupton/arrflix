<script setup lang="ts">
import { useQuery } from '@tanstack/vue-query'
import { Plus, Shield } from 'lucide-vue-next'
import { getV1PoliciesOptions } from '@/client/@tanstack/vue-query.gen'
import { type DbgenPolicy } from '@/client/types.gen'
import DataTable from '@/components/tables/DataTable.vue'
import { policyColumns, createPolicyActions } from '@/components/tables/configs/policyTableConfig'
import { useModal } from '@/composables/useModal'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import PolicyDialog from '@/components/modals/PolicyDialog.vue'
import RuleDialog from '@/components/modals/RuleDialog.vue'
import ActionsDialog from '@/components/modals/ActionsDialog.vue'

const modal = useModal()

// Data queries
const { data: policies, isLoading, refetch } = useQuery(getV1PoliciesOptions())

// Policy handlers
const handleAddPolicy = () => {
  modal.open(PolicyDialog, {
    props: {
      policy: null,
    },
    onClose: () => {
      refetch()
    },
  })
}

const handleEditPolicy = (policy: DbgenPolicy) => {
  modal.open(PolicyDialog, {
    props: {
      policy,
    },
    onClose: () => {
      refetch()
    },
  })
}

const handleEditRule = (policy: DbgenPolicy) => {
  modal.open(RuleDialog, {
    props: {
      policy,
    },
    onClose: () => {
      refetch()
    },
  })
}

const handleEditActions = (policy: DbgenPolicy) => {
  modal.open(ActionsDialog, {
    props: {
      policy,
    },
    onClose: () => {
      refetch()
    },
  })
}

const handleDeletePolicy = async (policy: DbgenPolicy) => {
  const confirmed = await modal.confirm({
    title: 'Delete Policy',
    message: `Are you sure you want to delete "${policy.name}"?`,
    severity: 'danger',
  })
  if (!confirmed) return
  // The delete will be handled by the DataTable actions
  refetch()
}

const policyActions = createPolicyActions(
  handleEditPolicy,
  handleEditRule,
  handleEditActions,
  handleDeletePolicy,
)
</script>

<template>
  <div class="flex flex-col gap-6">
    <Card>
      <CardHeader>
        <div class="flex items-center justify-between">
          <div>
            <CardTitle class="text-xl font-semibold mb-2">Policies</CardTitle>
            <p class="text-sm text-muted-foreground">
              Configure policies to automatically handle torrent downloads.
            </p>
          </div>
          <Button @click="handleAddPolicy">
            <Plus class="mr-2 size-4" />
            Add Policy
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
          :data="policies || []"
          :columns="policyColumns"
          :actions="policyActions"
          :loading="isLoading"
          empty-message="No policies configured"
          searchable
          search-placeholder="Search policies..."
          paginator
          :rows="10"
        >
          <template #empty-icon>
            <Shield class="size-5" />
          </template>
        </DataTable>
      </CardContent>
    </Card>
  </div>
</template>
