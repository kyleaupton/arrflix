<script setup lang="ts">
import AppSidebar from '@/components/AppSidebar.vue'
import AppLayoutHeader from '@/components/AppLayoutHeader.vue'
import { SidebarInset, SidebarProvider } from '@/components/ui/sidebar'
</script>

<template>
  <SidebarProvider>
    <AppSidebar />
    <SidebarInset>
      <AppLayoutHeader />
      <div class="flex flex-1 flex-col gap-4 p-4 pt-19 overflow-y-auto min-w-0">
        <slot />
      </div>
    </SidebarInset>
  </SidebarProvider>
</template>
