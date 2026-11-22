import { expect, test } from 'vitest'
import { screen } from '@testing-library/vue'
import userEvent from '@testing-library/user-event'
import type { Want } from '@/client/types.gen'
import { renderWithApp, type Viewer } from '@/test/render'
import TrackingActionsMenu from './TrackingActionsMenu.vue'

// Story 07, operator variant: Cancel acquisition and Stop tracking stop Arrflix
// looking, but a download already in the downloader finishes. Their
// confirmations say so, and point at the download's own Cancel.

const operator: Viewer = {
  sub: '00000000-0000-0000-0000-000000000002',
  permissions: ['jobs.manage'],
}

const downloadingWant: Want = {
  id: '30000000-0000-0000-0000-000000000001',
  trackingId: '20000000-0000-0000-0000-000000000001',
  mediaItemId: '40000000-0000-0000-0000-000000000001',
  qualityProfileId: '50000000-0000-0000-0000-000000000001',
  status: 'downloading',
  segment: 'backfill',
  attemptCount: 1,
  lastError: null,
  nextRunAt: '2026-11-01T12:00:00Z',
  createdAt: '2026-11-01T12:00:00Z',
  updatedAt: '2026-11-01T12:00:00Z',
}

async function openMenuItem(name: RegExp) {
  // Opened from the keyboard: jsdom lacks the pointer-capture API Reka's
  // pointer path calls.
  ;(await screen.findByRole('button', { name: /more actions/i })).focus()
  await userEvent.keyboard('{Enter}')
  await userEvent.click(await screen.findByRole('menuitem', { name }))
  return screen.findByRole('dialog')
}

test('Cancel acquisition confirmation says a download in progress finishes', async () => {
  renderWithApp(TrackingActionsMenu, {
    viewer: operator,
    props: {
      type: 'movie',
      tmdbId: 693134,
      trackingId: downloadingWant.trackingId,
      want: downloadingWant,
    },
  })

  const dialog = await openMenuItem(/cancel acquisition/i)

  expect(dialog).toHaveTextContent('Cancel acquisition')
  expect(dialog).toHaveTextContent(
    'Stop searching for this movie? A download already in progress will finish and be imported. To stop a download too, cancel it in Downloads.',
  )
})

test('Stop tracking confirmation says downloads in progress finish', async () => {
  renderWithApp(TrackingActionsMenu, {
    viewer: operator,
    props: {
      type: 'series',
      tmdbId: 1399,
      trackingId: '20000000-0000-0000-0000-000000000002',
      autonomyBackfill: 'auto',
      autonomyOngoing: 'auto',
    },
  })

  const dialog = await openMenuItem(/stop tracking/i)

  expect(dialog).toHaveTextContent('Stop tracking')
  expect(dialog).toHaveTextContent(
    'Stop looking for new episodes? Downloads already in progress will finish, and episodes you have are kept. To stop a download too, cancel it in Downloads.',
  )
})
