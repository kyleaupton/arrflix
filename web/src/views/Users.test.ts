import { expect, test } from 'vitest'
import { screen, waitFor, within } from '@testing-library/vue'
import userEvent from '@testing-library/user-event'
import { HttpResponse } from 'msw'
import {
  handleInvitesCreate,
  handleInvitesList,
  handleRolesList,
  handleUsersList,
  handleUsersUpdate,
  handleUsersUpdateRequestSettings,
} from '@/client/msw.gen'
import { server } from '@/test/server'
import { renderWithApp } from '@/test/render'
import { role, user } from '@/test/fixtures'
import Users from './Users.vue'

// The role catalog still holds the hidden roles; the pickers must not offer them.
const catalog = [role('admin'), role('co_admin'), role('requester'), role('viewer')]

async function pickerOptions(dialog: HTMLElement): Promise<string[]> {
  // Opened from the keyboard: jsdom lacks the pointer-capture API Reka's
  // pointer path calls.
  within(dialog).getByRole('combobox').focus()
  await userEvent.keyboard('{Enter}')
  const listbox = await screen.findByRole('listbox')
  return within(listbox)
    .getAllByRole('option')
    .map((option) => option.textContent?.trim() ?? '')
}

test('REQ-ROLE-001: the invite role picker offers exactly requester and admin', async () => {
  server.use(
    handleUsersList({ body: [] }),
    handleInvitesList({ body: [] }),
    handleRolesList({ body: catalog }),
  )
  renderWithApp(Users)

  await userEvent.click((await screen.findAllByRole('button', { name: /invite user/i }))[0]!)
  const dialog = await screen.findByRole('dialog')

  expect(within(dialog).getByRole('combobox')).toHaveTextContent('requester')
  expect(await pickerOptions(dialog)).toEqual(['requester', 'admin'])
})

test('REQ-ROLE-001: the user role picker offers exactly requester and admin, defaulting to requester', async () => {
  server.use(
    handleUsersList({ body: [user({ roles: null })] }),
    handleInvitesList({ body: [] }),
    handleRolesList({ body: catalog }),
  )
  renderWithApp(Users)
  ;(await screen.findByRole('button', { name: /open menu/i })).focus()
  await userEvent.keyboard('{Enter}')
  await userEvent.click(await screen.findByRole('menuitem', { name: /edit/i }))
  const dialog = await screen.findByRole('dialog')

  expect(within(dialog).getByRole('combobox')).toHaveTextContent('requester')
  expect(await pickerOptions(dialog)).toEqual(['requester', 'admin'])
})

// Chosen from the keyboard, for the same jsdom pointer-capture reason as above.
async function pickRole(dialog: HTMLElement, name: string) {
  within(dialog).getByRole('combobox').focus()
  await userEvent.keyboard('{Enter}')
  ;(await screen.findByRole('option', { name })).focus()
  await userEvent.keyboard('{Enter}')
  await waitFor(() => expect(within(dialog).getByRole('combobox')).toHaveTextContent(name))
}

async function openEditDialog(): Promise<HTMLElement> {
  ;(await screen.findByRole('button', { name: /open menu/i })).focus()
  await userEvent.keyboard('{Enter}')
  await userEvent.click(await screen.findByRole('menuitem', { name: /edit/i }))
  return screen.findByRole('dialog')
}

// Records each request-settings save, answering with the user as saved.
function captureSettingsSaves(saved: Array<{ autoApprove: boolean; allow4k: boolean }>) {
  return handleUsersUpdateRequestSettings(async ({ request }) => {
    const body = await request.json()
    saved.push(body)
    return HttpResponse.json(user(body))
  })
}

test('REQ-ROLE-004: the user dialog saves the auto-approve switch', async () => {
  const saved: Array<{ autoApprove: boolean; allow4k: boolean }> = []
  server.use(
    handleUsersList({ body: [user()] }),
    handleInvitesList({ body: [] }),
    handleUsersUpdate({ body: user() }),
    captureSettingsSaves(saved),
  )
  renderWithApp(Users)
  const dialog = await openEditDialog()

  const toggle = within(dialog).getByRole('switch', { name: /auto-approve requests/i })
  expect(toggle).toHaveAttribute('aria-checked', 'false')
  expect(within(dialog).getByText('Their requests skip the approval queue.')).toBeInTheDocument()
  await userEvent.click(toggle)
  await userEvent.click(within(dialog).getByRole('button', { name: 'Save' }))

  await waitFor(() => expect(saved).toEqual([{ autoApprove: true, allow4k: false }]))
})

test('REQ-ROLE-005: the user dialog saves the allow 4K switch', async () => {
  const saved: Array<{ autoApprove: boolean; allow4k: boolean }> = []
  server.use(
    handleUsersList({ body: [user({ autoApprove: true })] }),
    handleInvitesList({ body: [] }),
    handleUsersUpdate({ body: user() }),
    captureSettingsSaves(saved),
  )
  renderWithApp(Users)
  const dialog = await openEditDialog()

  const toggle = within(dialog).getByRole('switch', { name: /allow 4k/i })
  expect(within(dialog).getByText('They can request 4K versions.')).toBeInTheDocument()
  await userEvent.click(toggle)
  await userEvent.click(within(dialog).getByRole('button', { name: 'Save' }))

  await waitFor(() => expect(saved).toEqual([{ autoApprove: true, allow4k: true }]))
})

test('the user dialog hides the request settings for an admin and leaves them stored', async () => {
  const saved: Array<{ autoApprove: boolean; allow4k: boolean }> = []
  server.use(
    handleUsersList({ body: [user({ roles: [role('admin')], autoApprove: true })] }),
    handleInvitesList({ body: [] }),
    handleUsersUpdate({ body: user() }),
    captureSettingsSaves(saved),
  )
  renderWithApp(Users)
  const dialog = await openEditDialog()

  expect(within(dialog).queryByRole('switch', { name: /auto-approve requests/i })).toBeNull()
  expect(within(dialog).queryByRole('switch', { name: /allow 4k/i })).toBeNull()
  expect(
    within(dialog).getByText('Admins can request anything and skip approval.'),
  ).toBeInTheDocument()

  await userEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
  expect(saved).toEqual([])
})

test('the invite dialog sends its request settings, and hides them when inviting an admin', async () => {
  const sent: unknown[] = []
  server.use(
    handleUsersList({ body: [] }),
    handleInvitesList({ body: [] }),
    handleRolesList({ body: catalog }),
    handleInvitesCreate(async ({ request }) => {
      sent.push(await request.json())
      return HttpResponse.json(
        {
          invite: {
            id: '40000000-0000-0000-0000-000000000001',
            email: 'new@example.com',
            role: 'requester',
            autoApprove: true,
            allow4k: false,
            invitedBy: '00000000-0000-0000-0000-000000000001',
            createdAt: '2026-11-01T12:00:00Z',
            expiresAt: null,
            claimedAt: null,
          },
          token: 'tok',
          emailed: false,
        },
        { status: 201 },
      )
    }),
  )
  renderWithApp(Users)
  await userEvent.click((await screen.findAllByRole('button', { name: /invite user/i }))[0]!)
  const dialog = await screen.findByRole('dialog')

  const autoApprove = within(dialog).getByRole('switch', { name: /auto-approve requests/i })
  expect(autoApprove).toHaveAttribute('aria-checked', 'false')
  expect(within(dialog).getByRole('switch', { name: /allow 4k/i })).toHaveAttribute(
    'aria-checked',
    'false',
  )

  await pickRole(dialog, 'admin')
  expect(within(dialog).queryByRole('switch', { name: /auto-approve requests/i })).toBeNull()
  expect(
    within(dialog).getByText('Admins can request anything and skip approval.'),
  ).toBeInTheDocument()

  await pickRole(dialog, 'requester')
  await userEvent.click(within(dialog).getByRole('switch', { name: /auto-approve requests/i }))
  await userEvent.type(within(dialog).getByLabelText('Email'), 'new@example.com')
  await userEvent.click(within(dialog).getByRole('button', { name: 'Create Invite' }))

  await waitFor(() =>
    expect(sent).toEqual([
      { email: 'new@example.com', role: 'requester', autoApprove: true, allow4k: false },
    ]),
  )
})

test('the users list badges requesters who have auto-approve or 4K', async () => {
  server.use(
    handleUsersList({
      body: [
        user({ id: 'a', username: 'trusted', autoApprove: true, allow4k: true }),
        user({ id: 'b', username: 'plain', email: 'plain@example.com' }),
      ],
    }),
    handleInvitesList({ body: [] }),
  )
  renderWithApp(Users)

  const trustedRow = (await screen.findByText('trusted')).closest('tr')!
  expect(within(trustedRow).getByText('Auto-approve')).toBeInTheDocument()
  expect(within(trustedRow).getByText('4K')).toBeInTheDocument()
  const plainRow = screen.getByText('plain').closest('tr')!
  expect(within(plainRow).queryByText('Auto-approve')).toBeNull()
  expect(within(plainRow).queryByText('4K')).toBeNull()
})
