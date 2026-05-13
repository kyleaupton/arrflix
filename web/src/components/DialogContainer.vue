<script setup lang="ts">
import { useDialogStore } from '@/stores/dialog'
import DynamicDialog from './DynamicDialog.vue'

const { dialogs } = useDialogStore()
</script>

<template>
  <DynamicDialog v-for="dialog in dialogs" :key="dialog.id" :instance="dialog" />
</template>
