<script setup lang="ts">
import { ref } from 'vue'
import CircularProgress from '@/components/ui/progress/CircularProgress.vue'
import type {
  CircularProgressState,
  CircularProgressSize,
} from '@/components/ui/progress/CircularProgress.vue'

const progressValue = ref(45)
const states: CircularProgressState[] = [
  'progress',
  'indeterminate',
  'success',
  'error',
  'cancelled',
]
const sizes: CircularProgressSize[] = ['sm', 'md', 'lg']
</script>

<template>
  <div class="p-8 space-y-8 max-w-2xl">
    <h1 class="text-2xl font-bold">Dev Playground</h1>

    <section class="space-y-4">
      <h2 class="text-lg font-semibold">CircularProgress</h2>

      <div class="flex items-center gap-4">
        <span class="text-sm text-muted-foreground w-24">Value: {{ progressValue }}%</span>
        <input
          v-model.number="progressValue"
          type="range"
          min="0"
          max="100"
          step="1"
          class="w-64"
        />
      </div>

      <div class="border rounded-lg p-4">
        <table class="w-full">
          <thead>
            <tr>
              <th class="text-left text-sm text-muted-foreground pb-3">State</th>
              <th
                v-for="size in sizes"
                :key="size"
                class="text-center text-sm text-muted-foreground pb-3"
              >
                {{ size }}
              </th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="state in states" :key="state" class="border-t">
              <td class="py-3 text-sm font-mono">{{ state }}</td>
              <td v-for="size in sizes" :key="size" class="py-3 text-center">
                <div class="flex justify-center">
                  <CircularProgress :state="state" :value="progressValue" :size="size" />
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>
  </div>
</template>
