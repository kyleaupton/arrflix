import { expect, test } from 'vitest'
import { screen, within } from '@testing-library/vue'
import userEvent from '@testing-library/user-event'
import { handleNotificationsList, handleNotificationsUnreadCount } from '@/client/msw.gen'
import type { InboxNotification } from '@/client/types.gen'
import { server } from '@/test/server'
import { renderWithApp } from '@/test/render'
import { useAuthStore } from '@/stores/auth'
import NotificationBell from './NotificationBell.vue'

// The bell's queries run only for a signed-in session, which the token marks.
function renderBell() {
  renderWithApp(NotificationBell)
  useAuthStore().token = 'test-token'
}

function pendingRequest(overrides: Partial<InboxNotification> = {}): InboxNotification {
  return {
    id: crypto.randomUUID(),
    eventType: 'request.pending',
    title: 'New request: Arrival',
    body: 'req_a asked for Arrival (2016).',
    payload: { requestId: crypto.randomUUID(), media: { title: 'Arrival' } },
    createdAt: new Date().toISOString(),
    ...overrides,
  }
}

test('REQ-APPROVE-003: a decided request shows dimmed with its outcome and is not unread', async () => {
  const decided = pendingRequest({
    title: 'New request: Dune',
    resolvedAt: new Date().toISOString(),
    resolution: 'approved',
  })
  const open = pendingRequest({ title: 'New request: Arrival' })
  // Only the open request is unread: the API leaves resolved rows out of the count.
  server.use(
    handleNotificationsUnreadCount({ body: { count: 1 } }),
    handleNotificationsList({ body: [decided, open] }),
  )
  const user = userEvent.setup()
  renderBell()

  const bell = screen.getByRole('button', { name: 'Notifications' })
  expect(await within(bell).findByText('1')).toBeInTheDocument()
  await user.click(bell)

  const decidedRow = (await screen.findByText('New request: Dune')).closest('button')!
  expect(decidedRow).toHaveClass('opacity-60')
  expect(within(decidedRow).getByText(/Approved/)).toBeInTheDocument()
  expect(within(decidedRow).queryByLabelText('Unread')).toBeNull()

  const openRow = screen.getByText('New request: Arrival').closest('button')!
  expect(openRow).not.toHaveClass('opacity-60')
  expect(within(openRow).getByLabelText('Unread')).toBeInTheDocument()

  // Opening a resolved entry has nothing to clear: no mark-read call (MSW would
  // fail the test on an undeclared request) and the badge keeps its count.
  await user.click(decidedRow)
  expect(within(bell).getByText('1')).toBeInTheDocument()
})

test.each([
  ['denied', 'Declined'],
  ['withdrawn', 'Withdrawn'],
] as const)('a %s request is labelled %s in the bell', async (resolution, label) => {
  server.use(
    handleNotificationsUnreadCount({ body: { count: 0 } }),
    handleNotificationsList({
      body: [pendingRequest({ resolvedAt: new Date().toISOString(), resolution })],
    }),
  )
  const user = userEvent.setup()
  renderBell()

  await user.click(screen.getByRole('button', { name: 'Notifications' }))
  const row = (await screen.findByText('New request: Arrival')).closest('button')!
  expect(within(row).getByText(new RegExp(label))).toBeInTheDocument()
})
