import { expect, test } from 'vitest'
import { screen, waitFor } from '@testing-library/vue'
import userEvent from '@testing-library/user-event'
import { HttpResponse } from 'msw'
import { handleRequestsCancel, handleTitleStatusGet, handleTrackingByTmdb } from '@/client/msw.gen'
import { server } from '@/test/server'
import { renderWithApp } from '@/test/render'
import { notFound, request, titleStatus } from '@/test/fixtures'
import { promisesAcquisitionStops } from '@/test/copy'
import { openEventStream } from '@/test/realtime'
import AcquisitionControl from './AcquisitionControl.vue'

test('REQ-CANCEL-009: withdrawing from the focus page does not promise acquisition stops', async () => {
  const own = request({ status: 'spawned' })
  let withdrawn = false
  server.use(
    handleTitleStatusGet({
      body: titleStatus({
        state: 'searching',
        viewer: { isRequester: true, requestId: own.id },
        actions: [{ kind: 'cancel', enabled: true, requiresApproval: false }],
      }),
    }),
    handleTrackingByTmdb(notFound),
    handleRequestsCancel(() => {
      withdrawn = true
      return Response.json(request({ ...own, status: 'canceled' }))
    }),
  )
  renderWithApp(AcquisitionControl, { props: { tmdbId: own.tmdbId, title: 'Dune: Part Two' } })

  const withdraw = await screen.findByRole('button', { name: /withdraw/i })
  expect(promisesAcquisitionStops(withdraw.textContent ?? '')).toBe(false)
  await userEvent.click(withdraw)

  // Withdrawal either asks first or goes straight through; whichever copy the
  // viewer is shown on the way must not promise the work stops.
  await waitFor(() => expect(withdrawn || screen.queryByRole('dialog') !== null).toBe(true))
  const dialog = screen.queryByRole('dialog')
  if (dialog) {
    expect(promisesAcquisitionStops(dialog.textContent ?? '')).toBe(false)
  }
  expect(promisesAcquisitionStops(document.body.textContent ?? '')).toBe(false)
})

test('REQ-FULFIL-004: the status a requester sees follows a title-status kick without a reload', async () => {
  const own = request({ status: 'spawned' })
  let state = 'downloading'
  server.use(
    handleTitleStatusGet(() =>
      HttpResponse.json(titleStatus({ tmdbId: own.tmdbId, state, viewer: { isRequester: true } })),
    ),
    handleTrackingByTmdb(notFound),
  )
  renderWithApp(AcquisitionControl, {
    props: { tmdbId: own.tmdbId, title: 'Dune: Part Two' },
    realtime: true,
  })
  const send = openEventStream()

  expect(await screen.findByText('Downloading')).toBeInTheDocument()

  // The work moves on the server, which announces the title over the stream.
  state = 'available'
  send('title_status', { mediaType: 'movie', tmdbId: own.tmdbId })

  expect(await screen.findByText('In your library')).toBeInTheDocument()
  expect(screen.queryByText('Downloading')).not.toBeInTheDocument()
})
