import { expect, test } from 'vitest'
import { screen } from '@testing-library/vue'
import { handleNotificationPreferencesGet } from '@/client/msw.gen'
import type { BundlePreference } from '@/client/types.gen'
import { server } from '@/test/server'
import { renderWithApp } from '@/test/render'
import NotificationsView from './NotificationsView.vue'

function bundle(name: string, push: boolean): BundlePreference {
  return {
    bundle: name,
    subscribed: true,
    channels: [
      { channel: 'email', enabled: false, available: false },
      { channel: 'push', enabled: push, available: true },
    ],
  }
}

const userBundles = [bundle('library_activity', false), bundle('my_requests', true)]

test('the preferences page shows the admin alerts bundle to an admin', async () => {
  server.use(
    handleNotificationPreferencesGet({
      body: { bundles: [bundle('admin_alerts', true), ...userBundles] },
    }),
  )
  renderWithApp(NotificationsView, {
    viewer: {
      sub: '00000000-0000-0000-0000-000000000002',
      permissions: ['requests.view.any', 'requests.approve:movie', 'requests.deny:movie'],
    },
  })

  expect(await screen.findByText('Admin Alerts')).toBeInTheDocument()
  expect(screen.getByText('New requests waiting for your review.')).toBeInTheDocument()
  expect(screen.getByRole('switch', { name: 'Notify me about Admin Alerts' })).toBeInTheDocument()
  expect(screen.getByRole('switch', { name: 'Push for Admin Alerts' })).toBeInTheDocument()
})

test('the preferences page shows no admin alerts bundle to a requester', async () => {
  server.use(handleNotificationPreferencesGet({ body: { bundles: userBundles } }))
  renderWithApp(NotificationsView)

  expect(await screen.findByText('My Requests')).toBeInTheDocument()
  expect(screen.queryByText('Admin Alerts')).toBeNull()
  expect(screen.queryByText(/admin.alerts/i)).toBeNull()
})
