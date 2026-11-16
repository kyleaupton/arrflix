import { expect, test } from 'vitest'
import { screen, within } from '@testing-library/vue'
import userEvent from '@testing-library/user-event'
import { handleInvitesList, handleRolesList, handleUsersList } from '@/client/msw.gen'
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
