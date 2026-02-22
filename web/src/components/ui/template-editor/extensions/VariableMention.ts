import { mergeAttributes, Node } from '@tiptap/core'
import { VueNodeViewRenderer } from '@tiptap/vue-3'
import Suggestion, { type SuggestionOptions } from '@tiptap/suggestion'
import VariableMentionView from './VariableMentionView.vue'

export interface VariableMentionOptions {
  HTMLAttributes: Record<string, any>
  suggestion: Omit<SuggestionOptions, 'editor'>
}

declare module '@tiptap/core' {
  interface Commands<ReturnType> {
    variableMention: {
      /**
       * Insert a variable mention
       */
      insertVariableMention: (attributes: {
        path: string
        func?: 'clean' | 'sanitize' | null
        optional?: boolean
        prefix?: string
        suffix?: string
      }) => ReturnType
      /**
       * Update a variable mention's function
       */
      updateVariableMentionFunc: (pos: number, func: 'clean' | 'sanitize' | null) => ReturnType
      /**
       * Toggle the optional attribute of a variable mention
       */
      toggleVariableOptional: (pos: number) => ReturnType
      /**
       * Update optional, prefix, and suffix attributes
       */
      updateVariableOptional: (
        pos: number,
        attrs: { optional?: boolean; prefix?: string; suffix?: string },
      ) => ReturnType
    }
  }
}

export const VariableMention = Node.create<VariableMentionOptions>({
  name: 'variableMention',

  addOptions() {
    return {
      HTMLAttributes: {},
      suggestion: {
        char: '{',
        allowSpaces: true,
        startOfLine: false,
        items: () => [],
        render: () => ({}),
        command: () => {},
      },
    }
  },

  group: 'inline',

  inline: true,

  selectable: false,

  atom: true,

  addAttributes() {
    return {
      path: {
        default: '',
        parseHTML: (element) => element.getAttribute('data-path'),
        renderHTML: (attributes) => ({
          'data-path': attributes.path,
        }),
      },
      func: {
        default: null,
        parseHTML: (element) => element.getAttribute('data-func') || null,
        renderHTML: (attributes) => {
          if (!attributes.func) return {}
          return {
            'data-func': attributes.func,
          }
        },
      },
      optional: {
        default: false,
        parseHTML: (element) => element.getAttribute('data-optional') === 'true',
        renderHTML: (attributes) =>
          attributes.optional ? { 'data-optional': 'true' } : {},
      },
      prefix: {
        default: '',
        parseHTML: (element) => element.getAttribute('data-prefix') || '',
        renderHTML: (attributes) =>
          attributes.prefix ? { 'data-prefix': attributes.prefix } : {},
      },
      suffix: {
        default: '',
        parseHTML: (element) => element.getAttribute('data-suffix') || '',
        renderHTML: (attributes) =>
          attributes.suffix ? { 'data-suffix': attributes.suffix } : {},
      },
    }
  },

  parseHTML() {
    return [
      {
        tag: 'span[data-type="variable-mention"]',
      },
    ]
  },

  renderHTML({ node, HTMLAttributes }) {
    const displayValue = node.attrs.path.startsWith('.')
      ? node.attrs.path.slice(1)
      : node.attrs.path
    const label = node.attrs.func ? `${node.attrs.func} ${displayValue}` : displayValue

    return [
      'span',
      mergeAttributes(
        { 'data-type': 'variable-mention' },
        this.options.HTMLAttributes,
        HTMLAttributes,
      ),
      label,
    ]
  },

  addNodeView() {
    return VueNodeViewRenderer(VariableMentionView)
  },

  addProseMirrorPlugins() {
    return [
      Suggestion({
        editor: this.editor,
        ...this.options.suggestion,
      }),
    ]
  },

  addKeyboardShortcuts() {
    return {
      Backspace: () =>
        this.editor.commands.command(({ tr, state }) => {
          let isMention = false
          const { selection } = state
          const { empty, anchor } = selection

          if (!empty) {
            return false
          }

          state.doc.nodesBetween(anchor - 1, anchor, (node, pos) => {
            if (node.type.name === this.name) {
              isMention = true
              tr.insertText('', pos, pos + node.nodeSize)
              return false
            }
          })

          return isMention
        }),
    }
  },

  addCommands() {
    return {
      insertVariableMention:
        (attributes) =>
        ({ commands }) => {
          return commands.insertContent({
            type: this.name,
            attrs: attributes,
          })
        },
      updateVariableMentionFunc:
        (pos, func) =>
        ({ tr }) => {
          const node = tr.doc.nodeAt(pos)
          if (node && node.type.name === this.name) {
            tr.setNodeMarkup(pos, undefined, {
              ...node.attrs,
              func,
            })
            return true
          }
          return false
        },
      toggleVariableOptional:
        (pos) =>
        ({ tr }) => {
          const node = tr.doc.nodeAt(pos)
          if (node && node.type.name === this.name) {
            tr.setNodeMarkup(pos, undefined, {
              ...node.attrs,
              optional: !node.attrs.optional,
              // Reset prefix/suffix when toggling off
              ...(!node.attrs.optional ? {} : { prefix: '', suffix: '' }),
            })
            return true
          }
          return false
        },
      updateVariableOptional:
        (pos, attrs) =>
        ({ tr }) => {
          const node = tr.doc.nodeAt(pos)
          if (node && node.type.name === this.name) {
            tr.setNodeMarkup(pos, undefined, {
              ...node.attrs,
              ...attrs,
            })
            return true
          }
          return false
        },
    }
  },
})

