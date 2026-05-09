import { ref } from 'vue'
import type { Component } from 'vue'
import { useDialogStore } from '@/stores/dialog'
import ConfirmDialog from '@/components/modals/ConfirmDialog.vue'
import AlertDialog from '@/components/modals/AlertDialog.vue'

export interface ConfirmOptions {
  title?: string
  message: string
  confirmLabel?: string
  cancelLabel?: string
  severity?: 'danger' | 'warning' | 'info' | 'success' | 'secondary'
}

export interface AlertOptions {
  title?: string
  message: string
  severity?: 'info' | 'warning' | 'error' | 'success'
  okLabel?: string
}

export interface PromptOptions {
  title?: string
  message: string
  placeholder?: string
  defaultValue?: string
  confirmLabel?: string
  cancelLabel?: string
}

export interface DialogInstance {
  close: (data?: unknown) => void
  id: string
}

/**
 * Composable for managing modals using shadcn-vue Dialog
 */
export function useModal() {
  const dialogStore = useDialogStore()
  const currentDialogInstance = ref<DialogInstance | null>(null)

  /**
   * Show a confirmation dialog
   * @returns Promise that resolves to true if confirmed, false if cancelled
   */
  const confirm = (options: ConfirmOptions): Promise<boolean> => {
    return new Promise((resolve) => {
      const instance = dialogStore.openDialog(ConfirmDialog, {
        props: {
          title: options.title || 'Confirm',
          message: options.message,
          confirmLabel: options.confirmLabel,
          cancelLabel: options.cancelLabel,
          severity: options.severity,
        },
        onClose: (result) => {
          currentDialogInstance.value = null
          resolve((result?.data as { confirmed?: boolean })?.confirmed === true)
        },
      })
      currentDialogInstance.value = {
        close: (data?: unknown) => dialogStore.closeDialog(instance.id, data),
        id: instance.id,
      }
    })
  }

  /**
   * Show an alert dialog
   * @returns Promise that resolves when the dialog is closed
   */
  const alert = (options: AlertOptions): Promise<void> => {
    return new Promise((resolve) => {
      const instance = dialogStore.openDialog(AlertDialog, {
        props: {
          title: options.title || 'Alert',
          message: options.message,
          severity: options.severity,
          okLabel: options.okLabel,
        },
        onClose: () => {
          currentDialogInstance.value = null
          resolve()
        },
      })
      currentDialogInstance.value = {
        close: (data?: unknown) => dialogStore.closeDialog(instance.id, data),
        id: instance.id,
      }
    })
  }

  /**
   * Show a prompt dialog (input dialog)
   * Note: This requires a PromptDialog component to be created
   * @returns Promise that resolves to the input value or null if cancelled
   */
  const prompt = (options: PromptOptions): Promise<string | null> => {
    // For now, we'll use a simple implementation with window.prompt
    // A proper PromptDialog component can be added later
    return new Promise((resolve) => {
      const result = window.prompt(options.message, options.defaultValue || '')
      resolve(result)
    })
  }

  /**
   * Open a custom component as a modal
   * @returns DialogInstance with a close method
   */
  const open = <T = unknown>(
    component: Component,
    options?: {
      data?: unknown
      props?: Record<string, unknown>
      onClose?: (result?: { data?: T }) => void
    },
  ): DialogInstance => {
    const instance = dialogStore.openDialog(component, {
      data: options?.data,
      props: {
        ...options?.props,
      },
      onClose: (result?: { data?: unknown }) => {
        currentDialogInstance.value = null
        options?.onClose?.(result as { data?: T } | undefined)
      },
    })
    const dialogInstance: DialogInstance = {
      close: (data?: unknown) => dialogStore.closeDialog(instance.id, data),
      id: instance.id,
    }
    currentDialogInstance.value = dialogInstance
    return dialogInstance
  }

  /**
   * Close the currently open dialog
   * Note: This closes the last dialog opened via this composable instance.
   * For more control, use the instance returned from open() and call close() on it directly.
   * @param data Optional data to pass back to the onClose callback
   */
  const close = (data?: unknown) => {
    if (currentDialogInstance.value) {
      currentDialogInstance.value.close(data)
      currentDialogInstance.value = null
    }
  }

  return {
    confirm,
    alert,
    prompt,
    open,
    close,
  }
}
