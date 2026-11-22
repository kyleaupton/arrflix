import { expect, test } from 'vitest'
import { screen } from '@testing-library/vue'
import { handleTitleStatusGet, handleTrackingByTmdb } from '@/client/msw.gen'
import { server } from '@/test/server'
import { renderWithApp } from '@/test/render'
import { notFound, request, titleStatus } from '@/test/fixtures'
import SeriesAcquisitionControl from './SeriesAcquisitionControl.vue'

const own = request({ type: 'series', tmdbId: 424242, status: 'spawned' })

function renderSeries(state: string, available: number, offersCancel: boolean) {
  server.use(
    handleTitleStatusGet({
      body: titleStatus({
        mediaType: 'series',
        tmdbId: own.tmdbId,
        state,
        active: false,
        counts: { available, total: 2, working: 0 },
        library: { fileCount: available, hasFiles: available > 0 },
        viewer: { isRequester: true, requestId: own.id },
        actions: offersCancel ? [{ kind: 'cancel', enabled: true, requiresApproval: false }] : [],
      }),
    }),
    handleTrackingByTmdb(notFound),
  )
  renderWithApp(SeriesAcquisitionControl, { props: { tmdbId: own.tmdbId, title: 'Harbor Lights' } })
}

test('REQ-CANCEL-008: a fully available series offers no withdrawal', async () => {
  renderSeries('available', 2, false)

  expect(await screen.findByText('2 / 2 available')).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: /withdraw/i })).toBeNull()
})

test('REQ-CANCEL-008: a partly available series can still be withdrawn', async () => {
  renderSeries('partially_available', 1, true)

  expect(await screen.findByText('1 / 2 available')).toBeInTheDocument()
  expect(screen.getByRole('button', { name: /withdraw/i })).toBeEnabled()
})
