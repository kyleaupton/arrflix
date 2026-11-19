import { expect, test } from 'vitest'
import { screen, within } from '@testing-library/vue'
import userEvent from '@testing-library/user-event'
import { handleMediaGetMovie, handleRequestsList } from '@/client/msw.gen'
import { server } from '@/test/server'
import { renderWithApp } from '@/test/render'
import { notFound, request } from '@/test/fixtures'
import { promisesAcquisitionStops } from '@/test/copy'
import Requests from './Requests.vue'

test('REQ-CANCEL-009: the requests list withdraw confirmation does not promise acquisition stops', async () => {
  server.use(
    handleRequestsList({ body: [request({ status: 'spawned' })] }),
    handleMediaGetMovie(notFound),
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
