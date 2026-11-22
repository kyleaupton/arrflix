import { expect, test } from 'vitest'
import { screen } from '@testing-library/vue'
import { handleTitleStatusGet } from '@/client/msw.gen'
import type { MovieDetail } from '@/client/types.gen'
import { server } from '@/test/server'
import { renderWithApp } from '@/test/render'
import { titleStatus } from '@/test/fixtures'
import MovieStatusCard from './MovieStatusCard.vue'

function movie(overrides: Partial<MovieDetail> = {}): MovieDetail {
  return {
    tmdbId: 693134,
    title: 'Dune: Part Two',
    overview: '',
    status: 'Released',
    files: [],
    ...overrides,
  }
}

const requestedNotOut = titleStatus({
  state: 'unreleased',
  active: false,
  counts: { available: 0, total: 1, working: 0 },
  viewer: { isRequester: true },
})

test('REQ-UNREL-001: a requested movie with an announced home release says when it is expected', async () => {
  server.use(
    handleTitleStatusGet({ body: { ...requestedNotOut, homeReleaseDate: '2099-09-12T00:00:00Z' } }),
  )
  renderWithApp(MovieStatusCard, {
    props: { movie: movie({ releaseDates: { theatrical: '2099-07-03', digital: '2099-09-12' } }) },
  })

  expect(await screen.findByText('Expected 12 Sep')).toBeInTheDocument()
  expect(screen.queryByText(/searching/i)).not.toBeInTheDocument()
})

test('REQ-UNREL-001: a requested movie already in cinemas with no home release announced says so', async () => {
  server.use(handleTitleStatusGet({ body: requestedNotOut }))
  renderWithApp(MovieStatusCard, {
    props: {
      movie: movie({ releaseDate: '2020-07-03', releaseDates: { theatrical: '2020-07-03' } }),
    },
  })

  expect(await screen.findByText('In cinemas · home release not announced yet')).toBeInTheDocument()
  expect(screen.queryByText(/searching/i)).not.toBeInTheDocument()
})

test('REQ-UNREL-001: a requested movie not yet in cinemas gives its cinema date and no home date', async () => {
  server.use(handleTitleStatusGet({ body: requestedNotOut }))
  renderWithApp(MovieStatusCard, {
    props: {
      movie: movie({ releaseDate: '2099-07-03', releaseDates: { theatrical: '2099-07-03' } }),
    },
  })

  expect(
    await screen.findByText('In cinemas 3 Jul · home release not announced yet'),
  ).toBeInTheDocument()
  expect(screen.queryByText(/searching/i)).not.toBeInTheDocument()
})

const notRequested = titleStatus({
  state: 'not_requested',
  active: false,
  counts: { available: 0, total: 1, working: 0 },
})

test('a back-catalogue movie nobody requested shows its full release date and no home release', async () => {
  server.use(handleTitleStatusGet({ body: notRequested }))
  renderWithApp(MovieStatusCard, {
    props: {
      movie: movie({ releaseDate: '1975-06-20', releaseDates: { theatrical: '1975-06-20' } }),
    },
  })

  expect(await screen.findByText('Released Jun 20, 1975')).toBeInTheDocument()
  expect(screen.queryByText(/home release/i)).not.toBeInTheDocument()
})

test('a movie nobody requested that is in cinemas gives its cinema and digital dates', async () => {
  server.use(handleTitleStatusGet({ body: notRequested }))
  renderWithApp(MovieStatusCard, {
    props: {
      movie: movie({
        releaseDate: '2099-07-03',
        releaseDates: { theatrical: '2099-07-03', digital: '2099-09-12' },
      }),
    },
  })

  expect(await screen.findByText('In cinemas 3 Jul · digital release 12 Sep')).toBeInTheDocument()
})
