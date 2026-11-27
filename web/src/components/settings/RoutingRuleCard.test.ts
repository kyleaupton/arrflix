import { expect, test } from 'vitest'
import { screen, waitFor } from '@testing-library/vue'
import userEvent from '@testing-library/user-event'
import { HttpResponse } from 'msw'
import type { ProblemDetails, RoutingRuleWriteBody } from '@/client/types.gen'
import { handleIndexersListConfigured, handleRoutingUpdateRule } from '@/client/msw.gen'
import { server } from '@/test/server'
import { renderWithApp } from '@/test/render'
import { routingRule } from '@/test/fixtures'
import RoutingRuleCard from './RoutingRuleCard.vue'

function renderCard(rule = routingRule({ saveTo: '/data/torrents/4k' })) {
  renderWithApp(RoutingRuleCard, {
    props: { rule, fields: [], libraries: [], nameTemplates: [], downloaders: [] },
  })
}

async function openEditor() {
  await userEvent.click(screen.getByText('4K releases'))
  return screen.findByRole('textbox', { name: 'Save to' })
}

test("REQ-PATH-002: the rule editor shows the rule's save location with its hint", async () => {
  server.use(handleIndexersListConfigured({ body: [] }))
  renderCard()

  expect(screen.getByText(/download to \/data\/torrents\/4k/)).toBeInTheDocument()
  const input = await openEditor()
  expect(input).toHaveValue('/data/torrents/4k')
  expect(input).toHaveAccessibleDescription(
    "Folder as Arrflix sees it. Leave empty to use the downloader's default.",
  )
})

test('REQ-PATH-002: the rule editor saves the save location, and empty sends none', async () => {
  const saved: RoutingRuleWriteBody[] = []
  server.use(
    handleIndexersListConfigured({ body: [] }),
    handleRoutingUpdateRule(async ({ request }) => {
      const body = await request.json()
      saved.push(body)
      return HttpResponse.json(routingRule(body))
    }),
  )
  renderCard()

  const input = await openEditor()
  await userEvent.clear(input)
  await userEvent.type(input, '/data/torrents/uhd')
  await userEvent.click(screen.getByRole('button', { name: 'Save' }))
  await waitFor(() => expect(saved).toHaveLength(1))
  expect(saved[0]!.saveTo).toBe('/data/torrents/uhd')

  const again = await openEditor()
  await userEvent.clear(again)
  await userEvent.click(screen.getByRole('button', { name: 'Save' }))
  await waitFor(() => expect(saved).toHaveLength(2))
  expect(saved[1]).not.toHaveProperty('saveTo')
})

test("REQ-PATH-002: the rule editor shows the API's rejection of the save location", async () => {
  const problem: ProblemDetails = {
    type: 'about:blank',
    status: 422,
    title: 'Unprocessable Entity',
    detail: 'invalid routing rule',
    errors: [{ location: 'body.saveTo', message: 'must be an absolute path' }],
  }
  server.use(
    handleIndexersListConfigured({ body: [] }),
    handleRoutingUpdateRule(() => HttpResponse.json(problem, { status: 422 })),
  )
  renderCard()

  const input = await openEditor()
  await userEvent.clear(input)
  await userEvent.type(input, 'relative')
  await userEvent.click(screen.getByRole('button', { name: 'Save' }))

  expect(await screen.findByText('must be an absolute path')).toBeInTheDocument()
  expect(input).toHaveAttribute('aria-invalid', 'true')
})
