import { Extension } from '@tiptap/core'

export interface EditorHostContextOptions {
  notebook: string
  section: string
  page: string
  file_date: string
}

export const EditorHostContext = Extension.create<EditorHostContextOptions>({
  name: 'editorHostContext',

  addOptions() {
    return {
      notebook: '',
      section: '',
      page: '',
      file_date: ''
    }
  },

  addStorage() {
    return {
      notebook: this.options.notebook,
      section: this.options.section,
      page: this.options.page,
      file_date: this.options.file_date
    }
  }
})
