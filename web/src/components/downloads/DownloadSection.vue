<script setup lang="ts">
import { ref } from 'vue'
import { ChevronDown } from 'lucide-vue-next'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { Badge } from '@/components/ui/badge'

const props = withDefaults(
  defineProps<{
    title: string
    count: number
    collapsible?: boolean
    defaultOpen?: boolean
  }>(),
  {
    collapsible: false,
    defaultOpen: true,
  },
)

const isOpen = ref(props.defaultOpen)
</script>

<template>
  <div v-if="count > 0">
    <Collapsible v-if="collapsible" v-model:open="isOpen">
      <CollapsibleTrigger
        class="flex w-full items-center gap-2 py-2 text-sm font-medium hover:text-foreground/80 transition-colors"
      >
        <ChevronDown
          class="size-4 transition-transform"
          :class="{ '-rotate-90': !isOpen }"
        />
        <span>{{ title }}</span>
        <Badge variant="secondary" class="ml-1 text-xs">{{ count }}</Badge>
      </CollapsibleTrigger>
      <CollapsibleContent>
        <div class="grid gap-3 pt-2">
          <slot />
        </div>
      </CollapsibleContent>
    </Collapsible>

    <div v-else>
      <div class="flex items-center gap-2 py-2 text-sm font-medium">
        <span>{{ title }}</span>
        <Badge variant="secondary" class="text-xs">{{ count }}</Badge>
      </div>
      <div class="grid gap-3 pt-2">
        <slot />
      </div>
    </div>
  </div>
</template>
