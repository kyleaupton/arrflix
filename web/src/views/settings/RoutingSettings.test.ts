import { expect, test } from 'vitest'
import { screen, waitFor } from '@testing-library/vue'
import userEvent from '@testing-library/user-event'
import { HttpResponse } from 'msw'
import {
  handleDownloadersList,
  handleLibrariesList,
  handleNameTemplatesList,
  handleRoutingGetDefaults,
  handleRoutingGetFields,
  handleRoutingListRules,
  handleRoutingUpdateDefaults,
} from '@/client/msw.gen'
import { server } from '@/test/server'
import { renderWithApp } from '@/test/render'
import RoutingSettings from './RoutingSettings.vue'

function settingsReads(saveTo?: string) {
  return [
    handleRoutingListRules({ body: [] }),
    handleRoutingGetFields({ body: [] }),
    handleLibrariesList({ body: [] }),
    handleNameTemplatesList({ body: [] }),
    handleDownloadersList({ body: [] }),
    handleRoutingGetDefaults({ body: saveTo ? { saveTo } : {} }),
  ]
}

test('REQ-PATH-002: the routing defaults show an empty save location out of the box', async () => {
  server.use(...settingsReads())
  renderWithApp(RoutingSettings)

  const input = await screen.findByRole('textbox', { name: 'Save to' })
  expect(input).toHaveValue('')
  expect(input).toHaveAccessibleDescription('Empty: the downloader decides.')
})

test('REQ-PATH-002: the routing defaults show and save the save location', async () => {
  const saved: Array<{ saveTo?: string }> = []
  server.use(
    ...settingsReads('/data/torrents'),
    handleRoutingUpdateDefaults(async ({ request }) => {
      const body = await request.json()
      saved.push(body)
      return HttpResponse.json(body)
    }),
  )
  renderWithApp(RoutingSettings)

  const input = await screen.findByRole('textbox', { name: 'Save to' })
  await waitFor(() => expect(input).toHaveValue('/data/torrents'))
  await userEvent.clear(input)
  await userEvent.type(input, '/data/torrents/default')
  await userEvent.click(screen.getByRole('button', { name: 'Save defaults' }))

  await waitFor(() => expect(saved).toEqual([{ saveTo: '/data/torrents/default' }]))
  expect(await screen.findByText('Saved.')).toBeInTheDocument()
})
