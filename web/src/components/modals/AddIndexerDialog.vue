<script setup lang="ts">
import { ref, computed, inject } from 'vue'
import { useMutation } from '@tanstack/vue-query'
import {
  ChevronLeft,
  ChevronRight,
  Plus,
  X,
  Check,
  Settings,
  TableOfContents,
} from 'lucide-vue-next'
import { type IndexerOutput, type IndexerInput } from '@/client/types.gen'
import { indexersSaveMutation } from '@/client/@tanstack/vue-query.gen'
import { Button } from '@/components/ui/button'
import {
  Stepper,
  StepperItem,
  StepperTrigger,
  StepperIndicator,
  StepperTitle,
  StepperDescription,
  StepperSeparator,
} from '@/components/ui/stepper'
import SelectIndexerTypeStep from './steps/SelectIndexerTypeStep.vue'
import ConfigurationStep from './steps/ConfigurationStep.vue'
import ReviewStep from './steps/ReviewStep.vue'
import BaseDialog from './BaseDialog.vue'
import { client } from '@/client/client.gen'
import { useModal } from '@/composables/useModal'

const dialogRef = inject('dialogRef') as { value: { close: (data?: unknown) => void } }
const modal = useModal()

// Form state
const currentStep = ref(0)
const selectedIndexerType = ref<IndexerOutput | null>(null)
const saveData = ref<IndexerInput | undefined>(undefined)
const isTestingConfig = ref(false)

const createIndexerMutation = useMutation({
  ...indexersSaveMutation(),
  onSuccess: () => {
    dialogRef.value.close({ indexerAdded: true })
  },
  onError: (error) => {
    console.error('Failed to create indexer:', error)
  },
})

// Computed properties
const steps = computed(() => [
  {
    step: 1,
    label: 'Select Indexer',
    description: 'Choose from available indexers to configure.',
    icon: TableOfContents,
  },
  {
    step: 2,
    label: 'Configuration',
    description: 'Configure the specific settings for the selected indexer.',
    icon: Settings,
  },
  {
    step: 3,
    label: 'Review',
    description: 'Review your indexer configuration before creating.',
    icon: Check,
  },
])

const isLastStep = computed(() => currentStep.value === steps.value.length - 1)
const isFirstStep = computed(() => currentStep.value === 0)

const canProceed = computed(() => {
  switch (currentStep.value) {
    case 0:
      return selectedIndexerType.value !== null
    case 1:
      return true // Configuration step validation can be added later
    case 2:
      return true
    default:
      return false
  }
})

// Methods
const nextStep = () => {
  if (canProceed.value && !isLastStep.value) {
    currentStep.value++
  }
}

const prevStep = () => {
  if (!isFirstStep.value) {
    currentStep.value--
  }
}

const selectIndexerType = (indexer: IndexerOutput | null) => {
  selectedIndexerType.value = indexer
}

const handleTestIndexer = async () => {
  if (!saveData.value) {
    console.error('No configuration data to test')
    return
  }

  isTestingConfig.value = true

  try {
    const response = await client.post({
      url: '/v1/indexer/test',
      body: saveData.value,
    })

    const result = response.data as { success: boolean; message?: string; error?: string }

    if (result.success) {
      await modal.alert({
        title: 'Test Successful',
        message: result.message || 'Indexer connection test passed',
        severity: 'success',
      })
    } else {
      await modal.alert({
        title: 'Test Failed',
        message: result.error || 'Connection test failed',
        severity: 'error',
      })
    }
  } catch (err) {
    const error = err as { message?: string; data?: { error?: string } }
    await modal.alert({
      title: 'Test Failed',
      message: error.data?.error || error.message || 'Test failed',
      severity: 'error',
    })
  } finally {
    isTestingConfig.value = false
  }
}

const createIndexer = () => {
  if (canProceed.value && selectedIndexerType.value && saveData.value) {
    createIndexerMutation.mutate({
      body: saveData.value,
    })
  }
}

const closeModal = () => {
  dialogRef.value.close()
}
</script>

<template>
  <BaseDialog title="Add New Indexer">
    <!-- Progress Steps -->
    <div class="mb-6">
      <Stepper v-model="currentStep" class="w-full">
        <StepperItem
          v-for="item in steps"
          :key="item.step"
          :step="item.step - 1"
          class="relative flex w-full flex-col items-center justify-center"
        >
          <StepperTrigger>
            <StepperIndicator v-slot="{ step }" class="bg-muted">
              <template v-if="item.icon">
                <component :is="item.icon" class="w-4 h-4" />
              </template>
              <span v-else>{{ step }}</span>
            </StepperIndicator>
          </StepperTrigger>
          <StepperSeparator
            v-if="item.step !== steps[steps.length - 1]?.step"
            class="absolute left-[calc(50%+20px)] right-[calc(-50%+10px)] top-5 block h-0.5 shrink-0 rounded-full bg-muted group-data-[state=completed]:bg-primary"
          />
          <div class="flex flex-col items-center">
            <StepperTitle>
              {{ item.label }}
            </StepperTitle>
            <StepperDescription>
              {{ item.description }}
            </StepperDescription>
          </div>
        </StepperItem>
      </Stepper>
    </div>

    <!-- Step Content -->
    <div class="max-h-[calc(100vh*0.6)]">
      <!-- Step 1: Select Indexer Type -->
      <div v-if="currentStep === 0" class="step-1 h-full">
        <SelectIndexerTypeStep
          ref="selectStepRef"
          :selected-indexer="selectedIndexerType"
          @indexer-selected="selectIndexerType"
        />
      </div>

      <!-- Step 2: Configuration -->
      <div v-if="currentStep === 1 && selectedIndexerType" class="step-2">
        <ConfigurationStep v-model="saveData" :selected-indexer="selectedIndexerType" />
      </div>

      <!-- Step 3: Review -->
      <div v-if="currentStep === 2" class="step-3">
        <ReviewStep :selected-indexer="selectedIndexerType" :save-data="saveData" />
      </div>
    </div>

    <template #footer>
      <div class="flex justify-between items-center w-full">
        <Button variant="outline" @click="isFirstStep ? closeModal() : prevStep()">
          <X v-if="isFirstStep" class="mr-2 size-4" />
          <ChevronLeft v-else class="mr-2 size-4" />
          {{ isFirstStep ? 'Cancel' : 'Previous' }}
        </Button>

        <div class="flex gap-2">
          <Button
            v-if="currentStep === 1"
            variant="outline"
            :disabled="isTestingConfig || !saveData"
            @click="handleTestIndexer"
          >
            <Check class="mr-2 size-4" />
            Test
          </Button>
          <Button v-if="!isLastStep" :disabled="!canProceed" @click="nextStep">
            Next
            <ChevronRight class="ml-2 size-4" />
          </Button>
          <Button
            v-else
            :disabled="!canProceed || createIndexerMutation.isPending.value"
            @click="createIndexer"
          >
            <Plus class="mr-2 size-4" />
            Create Indexer
          </Button>
        </div>
      </div>
    </template>
  </BaseDialog>
</template>
