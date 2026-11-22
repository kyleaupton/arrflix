import { expect, test } from 'vitest'
import { screen, within } from '@testing-library/vue'
import userEvent from '@testing-library/user-event'
import { HttpResponse } from 'msw'
import { handleMediaGetMovie, handleRequestsList, handleTitleStatusGet } from '@/client/msw.gen'
import { server } from '@/test/server'
import { renderWithApp } from '@/test/render'
import { notFound, request, titleStatus } from '@/test/fixtures'
import { promisesAcquisitionStops } from '@/test/copy'
import Requests from './Requests.vue'

test('REQ-CANCEL-009: the requests list withdraw confirmation does not promise acquisition stops', async () => {
  const own = request({ status: 'spawned' })
  server.use(
    handleRequestsList({ body: [own] }),
    handleMediaGetMovie(notFound),
    handleTitleStatusGet({
      body: titleStatus({
        viewer: { isRequester: true, requestId: own.id },
        actions: [{ kind: 'cancel', enabled: true, requiresApproval: false }],
      }),
    }),
  )
  renderWithApp(Requests)

  await userEvent.click(await screen.findByRole('button', { name: /^(cancel|withdraw)/i }))

  const dialog = await screen.findByRole('dialog')
  expect(dialog).toHaveTextContent(/\S/)
  expect(promisesAcquisitionStops(dialog.textContent ?? '')).toBe(false)
  // The confirm button's label is copy too.
  for (const button of within(dialog).getAllByRole('button')) {
    expect(promisesAcquisitionStops(button.textContent ?? '')).toBe(false)
  }
})

test('REQ-APPROVE-005: the review queue shows a reviewer only requests they can decide, and a requester none', async () => {
  const pending = [
    request({ id: '10000000-0000-0000-0000-000000000011', status: 'pending', requestedBy: 'u-a' }),
    request({ id: '10000000-0000-0000-0000-000000000012', status: 'pending', requestedBy: 'u-b' }),
  ]
  server.use(handleRequestsList({ body: pending }), handleMediaGetMovie(notFound))

  // A requester holds no review authority: there is no queue to see.
  const { unmount } = renderWithApp(Requests)
  await screen.findByRole('tab', { name: 'My Requests' })
  expect(screen.queryByRole('tab', { name: 'Review Queue' })).toBeNull()
  unmount()

  renderWithApp(Requests, {
    viewer: {
      sub: '00000000-0000-0000-0000-000000000002',
      permissions: ['requests.view.any', 'requests.approve:movie', 'requests.deny:movie'],
    },
  })
  await userEvent.click(await screen.findByRole('tab', { name: 'Review Queue' }))
  const panel = await screen.findByRole('tabpanel')
  await within(panel).findAllByRole('button', { name: 'Approve' })
  // Every queued request carries the controls to decide it.
  expect(within(panel).getAllByRole('button', { name: 'Approve' })).toHaveLength(pending.length)
  expect(within(panel).getAllByRole('button', { name: 'Deny' })).toHaveLength(pending.length)
})

test('REQ-CANCEL-008: the requests list offers no withdrawal for a title already available', async () => {
  // The two rows are told apart by tier: the 4K request's title is available.
  const inLibrary = request({
    id: '10000000-0000-0000-0000-000000000021',
    tmdbId: 1001,
    tier: '4K',
  })
  const searching = request({ id: '10000000-0000-0000-0000-000000000022', tmdbId: 1002 })
  server.use(
    handleRequestsList({ body: [inLibrary, searching] }),
    handleMediaGetMovie(notFound),
    // The server stops offering withdrawal once a title is available; the
    // searching title still offers it.
    handleTitleStatusGet(({ params }) => {
      const available = Number(params.tmdbId) === inLibrary.tmdbId
      const own = available ? inLibrary : searching
      return HttpResponse.json(
        titleStatus({
          tmdbId: own.tmdbId,
          state: available ? 'available' : 'searching',
          viewer: { isRequester: true, requestId: own.id },
          actions: available ? [] : [{ kind: 'cancel', enabled: true, requiresApproval: false }],
        }),
      )
    }),
  )
  renderWithApp(Requests)

  const row = (tier: string) => screen.getByText(tier).closest('.rounded-lg') as HTMLElement
  const withdraw = /^(cancel|withdraw)/i

  // Once the still-searching row offers withdrawal, both titles' status has loaded.
  await screen.findByText('HD')
  expect(await within(row('HD')).findByRole('button', { name: withdraw })).toBeInTheDocument()
  expect(within(row('4K')).queryByRole('button', { name: withdraw })).toBeNull()
  expect(screen.getAllByRole('button', { name: withdraw })).toHaveLength(1)
})
