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
