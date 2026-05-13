<script setup lang="ts">
import { ref } from 'vue'
import { useQuery } from '@tanstack/vue-query'
import { Plus, Shield } from 'lucide-vue-next'
import {
  policiesListOptions,
  policiesGetFieldsOptions,
  librariesListOptions,
  nameTemplatesListOptions,
  downloadersListOptions,
} from '@/client/@tanstack/vue-query.gen'
import PolicyCard from '@/components/settings/PolicyCard.vue'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'

// Data queries
const { data: policies, isLoading: policiesLoading, refetch } = useQuery(policiesListOptions())
const { data: fields, isLoading: fieldsLoading } = useQuery(policiesGetFieldsOptions())
const { data: libraries } = useQuery(librariesListOptions())
const { data: nameTemplates } = useQuery(nameTemplatesListOptions())
const { data: downloaders } = useQuery(downloadersListOptions())

// New policy cards
const newPolicies = ref<null[]>([])

const handleAddPolicy = () => {
  newPolicies.value.push(null)
}

const handleSaved = () => {
  refetch()
}

const handleNewSaved = (index: number) => {
  newPolicies.value.splice(index, 1)
  refetch()
}

const handleDeleted = () => {
  refetch()
}

const removeNew = (index: number) => {
  newPolicies.value.splice(index, 1)
}
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
        <div v-if="policiesLoading || fieldsLoading" class="space-y-3">
          <Skeleton class="h-12 w-full" />
          <Skeleton class="h-12 w-full" />
          <Skeleton class="h-12 w-full" />
        </div>

        <template v-else>
          <div v-if="(!policies || policies.length === 0) && newPolicies.length === 0" class="py-8">
            <Empty>
              <EmptyMedia variant="icon">
                <Shield class="size-5" />
              </EmptyMedia>
              <EmptyContent>
                <EmptyHeader>
                  <EmptyTitle>No policies configured</EmptyTitle>
                </EmptyHeader>
                <EmptyDescription>
                  Click "Add Policy" to create your first policy.
                </EmptyDescription>
              </EmptyContent>
            </Empty>
          </div>

          <div v-else class="space-y-3">
            <!-- Existing policies -->
            <PolicyCard
              v-for="policy in policies"
              :key="policy.id"
              :policy="policy"
              :fields="fields || []"
              :libraries="libraries || []"
              :name-templates="nameTemplates || []"
              :downloaders="downloaders || []"
              @saved="handleSaved"
              @deleted="handleDeleted"
            />

            <!-- New policy cards -->
            <PolicyCard
              v-for="(_, idx) in newPolicies"
              :key="`new-${idx}`"
              :policy="null"
              :fields="fields || []"
              :libraries="libraries || []"
              :name-templates="nameTemplates || []"
              :downloaders="downloaders || []"
              :is-new="true"
              @saved="handleNewSaved(idx)"
              @cancelled="removeNew(idx)"
              @deleted="removeNew(idx)"
            />
          </div>
        </template>
      </CardContent>
    </Card>
  </div>
</template>
