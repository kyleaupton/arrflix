import { VueRenderer } from '@tiptap/vue-3'
import tippy, { type Instance as TippyInstance } from 'tippy.js'
import type { SuggestionOptions, SuggestionProps } from '@tiptap/suggestion'
import VariableAutocompleteSuggestion from './VariableAutocompleteSuggestion.vue'
import type { TemplateVariable } from '@/composables/useTemplateVariables'

export interface VariableSuggestionProps {
  mediaType: 'movie' | 'series'
}

interface ExtendedSuggestionProps extends SuggestionProps {
  mediaType?: 'movie' | 'series'
}

export function variableSuggestion(
  config: VariableSuggestionProps,
): Omit<SuggestionOptions, 'editor'> {
  return {
    char: '{',
    allowSpaces: true,
    startOfLine: false,

    items: ({ query }) => {
      // Items are handled by the Vue component
      return []
    },

    render: () => {
      let component: VueRenderer
      let popup: TippyInstance[]

      return {
        onStart: (props: ExtendedSuggestionProps) => {
          component = new VueRenderer(VariableAutocompleteSuggestion, {
            props: {
              ...props,
              mediaType: config.mediaType,
            },
            editor: props.editor,
          })

          if (!props.clientRect) {
            return
          }

          const rect = props.clientRect()
          if (!rect) return

          popup = tippy(document.body, {
            getReferenceClientRect: () => rect,
            appendTo: () => document.body,
            content: component.element as Element,
            showOnCreate: true,
            interactive: true,
            trigger: 'manual',
            placement: 'bottom-start',
            maxWidth: 'none',
          }) as unknown as TippyInstance[]
        },

        onUpdate(props: ExtendedSuggestionProps) {
          component.updateProps({
            ...props,
            mediaType: config.mediaType,
          })

          if (!props.clientRect) {
            return
          }

          const rect = props.clientRect()
          if (!rect || !popup?.[0]) return

          popup[0].setProps({
            getReferenceClientRect: () => rect,
          })
        },

        onKeyDown(props) {
          if (props.event.key === 'Escape') {
            popup?.[0]?.hide()
            return true
          }

          const ref = component.ref as { onKeyDown?: (p: unknown) => boolean } | null
          return ref?.onKeyDown?.(props) ?? false
        },

        onExit() {
          popup?.[0]?.destroy()
          component.destroy()
        },
      }
    },

    command: ({ editor, range, props }) => {
      const variable = props as unknown as TemplateVariable

      // Delete the trigger character and query
      editor
        .chain()
        .focus()
        .deleteRange(range)
        .insertVariableMention({
          path: variable.path,
          func: null,
        })
        .run()
    },
  }
}
