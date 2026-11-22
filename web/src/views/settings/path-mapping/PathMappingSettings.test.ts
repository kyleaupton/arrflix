import { expect, test } from 'vitest'
import { screen, waitFor, within } from '@testing-library/vue'
import userEvent from '@testing-library/user-event'
import { HttpResponse } from 'msw'
import {
  handlePathMappingChecks,
  handlePathMappingPreview,
  handlePathMappingTest,
  handleVolumesCreate,
  handleVolumesList,
} from '@/client/msw.gen'
import type { PathMappingTestData, VolumesCreateData } from '@/client/types.gen'
import { server } from '@/test/server'
import { renderWithApp, type Viewer } from '@/test/render'
import { pathCheck, pathTestResult, volume } from '@/test/fixtures'
import PathMappingSettings from './PathMappingSettings.vue'

const admin: Viewer = {
  sub: '00000000-0000-0000-0000-0000000000aa',
  permissions: ['admin.settings.read', 'admin.settings.write'],
}

const qbit = pathCheck({
  downloaderId: '40000000-0000-0000-0000-000000000001',
  downloaderName: 'qBittorrent',
  status: 'no_mapping_needed',
  savePath: '/data/torrents',
  resolvedPath: '/data/torrents',
})
const sab = pathCheck({
  downloaderId: '40000000-0000-0000-0000-000000000002',
  downloaderName: 'Remote qBit',
  status: 'not_visible',
  savePath: '/downloads',
  resolvedPath: '/downloads',
})
const mapped = pathCheck({
  downloaderId: '40000000-0000-0000-0000-000000000003',
  downloaderName: 'Seedbox',
  status: 'mapped',
  savePath: '/srv/dl',
  resolvedPath: '/data/dl',
  volumeName: 'media',
})

test('REQ-PATH-003: each downloader shows whether a mapping is needed', async () => {
  server.use(
    handlePathMappingChecks({ body: [qbit, sab, mapped] }),
    handleVolumesList({ body: [] }),
  )
  renderWithApp(PathMappingSettings, { viewer: admin })

  const row = (id: string) => screen.findByTestId(`path-check-${id}`)
  expect(await row(qbit.downloaderId)).toHaveTextContent('✓ No mapping needed')
  expect(await row(mapped.downloaderId)).toHaveTextContent('✓ Mapped via media')
  expect(await row(sab.downloaderId)).toHaveTextContent(
    "⚠ Arrflix can't see Remote qBit's downloads at /downloads. Add a path mapping.",
  )
  expect(await screen.findByText(/No shared locations/)).toBeInTheDocument()
})

test('an admin adds a shared location with a downloader alias and sees a live preview', async () => {
  const saved: VolumesCreateData['body'][] = []
  server.use(
    handlePathMappingChecks({ body: [sab] }),
    handleVolumesList({ body: [] }),
    handlePathMappingPreview(async ({ request }) => {
      const body = await request.json()
      const alias = body.aliases?.find((a) => a.downloaderId === sab.downloaderId)
      return HttpResponse.json([
        alias ? { ...sab, status: 'mapped' as const, resolvedPath: body.arrflixBase } : sab,
      ])
    }),
    handleVolumesCreate(async ({ request }) => {
      const body = await request.json()
      saved.push(body)
      return HttpResponse.json(volume({ name: body.name, arrflixBase: body.arrflixBase }), {
        status: 201,
      })
    }),
  )
  renderWithApp(PathMappingSettings, { viewer: admin })

  await userEvent.click(await screen.findByRole('button', { name: /add location/i }))
  const dialog = await screen.findByRole('dialog')
  await userEvent.type(within(dialog).getByLabelText('Name'), 'media')
  await userEvent.type(within(dialog).getByLabelText('Path in Arrflix'), '/data')
  await userEvent.type(within(dialog).getByLabelText('Remote qBit sees it at'), '/downloads')

  await waitFor(() =>
    expect(within(dialog).getByTestId('location-preview-row')).toHaveTextContent(
      'Remote qBit: /downloads → /data ✓ exists',
    ),
  )

  await userEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
  await waitFor(() =>
    expect(saved).toEqual([
      {
        name: 'media',
        arrflixBase: '/data',
        aliases: [{ downloaderId: sab.downloaderId, base: '/downloads' }],
      },
    ]),
  )
})

test('a viewer without settings write sees the checks but cannot add a location', async () => {
  server.use(handlePathMappingChecks({ body: [qbit] }), handleVolumesList({ body: [] }))
  renderWithApp(PathMappingSettings, {
    viewer: { sub: admin.sub, permissions: ['admin.settings.read'] },
  })

  expect(await screen.findByText('✓ No mapping needed')).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: /add location/i })).not.toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Test qBittorrent' })).toBeInTheDocument()
})

test('REQ-PATH-004: Test reports hardlinks OK, copy, or not found', async () => {
  const tested: PathMappingTestData['body'][] = []
  server.use(
    handlePathMappingChecks({ body: [qbit] }),
    handleVolumesList({ body: [] }),
    handlePathMappingTest(async ({ request }) => {
      const body = await request.json()
      tested.push(body)
      if (body.path === '/data/missing') {
        return HttpResponse.json(
          pathTestResult({ path: body.path, resolvedPath: body.path, exists: false }),
        )
      }
      return HttpResponse.json(
        pathTestResult({
          path: body.path,
          resolvedPath: body.path,
          volumeName: 'media',
          libraries: [
            {
              libraryId: 'l1',
              libraryName: 'Movies',
              rootPath: '/data/movies',
              verdict: 'hardlink',
            },
            { libraryId: 'l2', libraryName: 'Series', rootPath: '/tv', verdict: 'copy' },
          ],
        }),
      )
    }),
  )
  renderWithApp(PathMappingSettings, { viewer: admin })

  await userEvent.click(await screen.findByRole('button', { name: 'Test qBittorrent' }))
  const input = screen.getByLabelText('Path as qBittorrent sees it')
  expect(input).toHaveValue('/data/torrents')
  await userEvent.click(screen.getByRole('button', { name: 'Run test' }))

  const result = await screen.findByTestId('path-test-result')
  expect(tested[0]).toEqual({ downloaderId: qbit.downloaderId, path: '/data/torrents' })
  expect(result).toHaveTextContent('(via media)')
  expect(result).toHaveTextContent('✓ Found in Arrflix')
  expect(result).toHaveTextContent('Movies: ✓ Hardlinks OK')
  expect(result).toHaveTextContent('Series: ⚠ Different filesystem: imports will copy')

  await userEvent.clear(input)
  await userEvent.type(input, '/data/missing')
  await userEvent.click(screen.getByRole('button', { name: 'Run test' }))

  await waitFor(() =>
    expect(screen.getByTestId('path-test-result')).toHaveTextContent('✗ Not found in Arrflix'),
  )
  expect(screen.getByTestId('path-test-result')).not.toHaveTextContent('Movies')
})
