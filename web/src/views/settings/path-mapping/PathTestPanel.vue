<script setup lang="ts">
import { ref } from 'vue'
import { useMutation } from '@tanstack/vue-query'
import { pathMappingTestMutation } from '@/client/@tanstack/vue-query.gen'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { problemMessage } from '@/lib/api'

const props = defineProps<{
  downloaderId: string
  downloaderName: string
  defaultPath: string
}>()

const path = ref(props.defaultPath)
const error = ref<string | null>(null)
const inputId = `path-test-${props.downloaderId}`

const test = useMutation({
  ...pathMappingTestMutation(),
  onSuccess: () => {
    error.value = null
  },
  onError: (err) => {
    error.value = problemMessage(err, 'Test failed')
  },
})

function run() {
  if (!path.value) return
  test.mutate({ body: { downloaderId: props.downloaderId, path: path.value } })
}

defineExpose({ run })
</script>

<template>
  <div class="flex flex-col gap-3 rounded-md border bg-muted/30 p-3">
    <form class="flex flex-col gap-2" @submit.prevent="run">
      <Label :for="inputId">Path as {{ downloaderName }} sees it</Label>
      <div class="flex gap-2">
        <Input :id="inputId" v-model="path" placeholder="/downloads" />
        <Button type="submit" variant="outline" :disabled="test.isPending.value || !path">
          {{ test.isPending.value ? 'Testing…' : 'Run test' }}
        </Button>
      </div>
    </form>
    <p v-if="error" class="text-sm text-destructive">{{ error }}</p>
    <div
      v-else-if="test.data.value"
      class="flex flex-col gap-1 text-sm"
      data-testid="path-test-result"
    >
      <p>
        Arrflix path: <code class="break-all">{{ test.data.value.resolvedPath }}</code>
        <span class="text-muted-foreground">
          {{
            test.data.value.volumeName
              ? `(via ${test.data.value.volumeName})`
              : '(no mapping applied)'
          }}
        </span>
      </p>
      <p v-if="!test.data.value.exists" class="text-destructive">✗ Not found in Arrflix</p>
      <template v-else>
        <p class="text-green-600 dark:text-green-500">✓ Found in Arrflix</p>
        <p v-if="!test.data.value.libraries?.length" class="text-muted-foreground">
          No libraries to compare with.
        </p>
        <ul class="flex flex-col gap-1">
          <li v-for="lib in test.data.value.libraries ?? []" :key="lib.libraryId">
            <span class="font-medium">{{ lib.libraryName }}:</span>
            <span v-if="lib.verdict === 'hardlink'" class="text-green-600 dark:text-green-500">
              ✓ Hardlinks OK
            </span>
            <span v-else-if="lib.verdict === 'copy'" class="text-amber-600 dark:text-amber-500">
              ⚠ Different filesystem: imports will copy
            </span>
            <span v-else class="text-amber-600 dark:text-amber-500">
              ⚠ Couldn't compare with {{ lib.rootPath }}
            </span>
          </li>
        </ul>
      </template>
    </div>
  </div>
</template>
