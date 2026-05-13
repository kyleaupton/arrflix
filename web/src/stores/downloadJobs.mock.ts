import type { DownloadJob } from './downloadJobs'

type MockMedia = {
  title: string
  year: number
  type: string
  poster: string
  cert: string
  candidate: string
  season?: number
  episode?: number
}

// Real TMDB poster paths for recognizable titles
const MOCK_MEDIA: MockMedia[] = [
  {
    title: 'Inception',
    year: 2010,
    type: 'movie',
    poster: '/edv5CZvWj09upOy2maq0Qs58RDI.jpg',
    cert: 'PG-13',
    candidate: 'Inception.2010.2160p.UHD.BluRay.x265-GROUP',
  },
  {
    title: 'The Dark Knight',
    year: 2008,
    type: 'movie',
    poster: '/qJ2tW6WMUDux911BTUgMe1nREA.jpg',
    cert: 'PG-13',
    candidate: 'The.Dark.Knight.2008.1080p.BluRay.x264-GROUP',
  },
  {
    title: 'Interstellar',
    year: 2014,
    type: 'movie',
    poster: '/gEU2QniE6E77NI6lCU6MxlNBvIx.jpg',
    cert: 'PG-13',
    candidate: 'Interstellar.2014.2160p.UHD.BluRay.Remux.HDR.HEVC.DTS-HD.MA-GROUP',
  },
  {
    title: 'Breaking Bad',
    year: 2008,
    type: 'series',
    poster: '/ztkUQFLlC19CCMYHW9o1zp7XMAK.jpg',
    cert: 'TV-MA',
    candidate: 'Breaking.Bad.S05E16.Felina.1080p.BluRay.x264-GROUP',
    season: 5,
    episode: 16,
  },
  {
    title: 'Blade Runner 2049',
    year: 2017,
    type: 'movie',
    poster: '/gajva2L0rPYkEWjzgFlBXCAVBE5.jpg',
    cert: 'R',
    candidate: 'Blade.Runner.2049.2017.1080p.BluRay.x264-GROUP',
  },
  {
    title: 'Dune',
    year: 2021,
    type: 'movie',
    poster: '/d5NXSklXo0qyIYkgV94XAgMIckC.jpg',
    cert: 'PG-13',
    candidate: 'Dune.2021.2160p.WEB-DL.DDP5.1.Atmos.HDR.H.265-GROUP',
  },
  {
    title: 'Severance',
    year: 2022,
    type: 'series',
    poster: '/lFGgqFhRCoCjMlk1sVuIFDzflsu.jpg',
    cert: 'TV-MA',
    candidate: 'Severance.S01E09.The.We.We.Are.1080p.ATVP.WEB-DL.DDP5.1.H.264-GROUP',
    season: 1,
    episode: 9,
  },
  {
    title: 'The Matrix',
    year: 1999,
    type: 'movie',
    poster: '/f89U3ADr1oiB1s9GkdPOEpXUk5H.jpg',
    cert: 'R',
    candidate: 'The.Matrix.1999.2160p.UHD.BluRay.Remux.HDR.HEVC.DTS-HD.MA-GROUP',
  },
  {
    title: 'Chernobyl',
    year: 2019,
    type: 'series',
    poster: '/hlLXt2tOPT6RRnjiUmoxyG1LTFi.jpg',
    cert: 'TV-MA',
    candidate: 'Chernobyl.S01E05.Vichnaya.Pamyat.1080p.AMZN.WEB-DL.DDP5.1.H.264-GROUP',
    season: 1,
    episode: 5,
  },
]

function makeMockJob(index: number, overrides: Partial<DownloadJob>): DownloadJob {
  const media = MOCK_MEDIA[index % MOCK_MEDIA.length]!
  const now = new Date()
  const updatedAt = new Date(now.getTime() - index * 60_000) // stagger by 1 min each

  return {
    id: crypto.randomUUID(),
    activeImports: 0,
    attemptCount: 1,
    cancelledImports: 0,
    candidateLink: '',
    candidateTitle: media.candidate,
    completedImports: 0,
    contentPath: `/downloads/${media.candidate}`,
    createdAt: new Date(updatedAt.getTime() - 600_000).toISOString(),
    downloadSpeed: 0,
    downloaderExternalId: '',
    downloaderId: crypto.randomUUID(),
    downloaderStatus: '',
    episodeId: media.type === 'series' ? crypto.randomUUID() : '',
    episodeNumber: media.type === 'series' ? (media.episode ?? 0) : 0,
    errorKind: null,
    etaSeconds: 0,
    failedImports: 0,
    guid: crypto.randomUUID(),
    importStatus: 'download_pending',
    indexerId: 1,
    lastError: '',
    libraryId: crypto.randomUUID(),
    mediaCertification: media.cert,
    mediaItemId: crypto.randomUUID(),
    mediaPosterPath: media.poster,
    mediaTitle: media.title,
    mediaType: media.type,
    mediaYear: media.year,
    nameTemplateId: crypto.randomUUID(),
    nextRunAt: '',
    pendingImports: 0,
    previousJobId: '',
    progress: 0,
    protocol: 'torrent',
    savePath: '/downloads',
    seasonId: media.type === 'series' ? crypto.randomUUID() : '',
    seasonNumber: media.type === 'series' ? (media.season ?? 0) : 0,
    status: 'downloading',
    tmdbId: 1000 + index,
    totalImportTasks: 0,
    totalSize: 0,
    updatedAt: updatedAt.toISOString(),
    ...overrides,
  }
}

export function createMockJobs(): DownloadJob[] {
  return [
    // Active: just started (0%)
    makeMockJob(0, {
      importStatus: 'download_pending',
      status: 'downloading',
      progress: 0,
      downloadSpeed: 0,
      etaSeconds: 0,
      totalSize: 15_000_000_000,
    }),

    // Active: mid-download (45%)
    makeMockJob(1, {
      importStatus: 'download_pending',
      status: 'downloading',
      progress: 0.45,
      downloadSpeed: 25_000_000,
      etaSeconds: 330,
      totalSize: 8_500_000_000,
    }),

    // Failed download
    makeMockJob(2, {
      importStatus: 'download_failed',
      status: 'failed',
      progress: 0.72,
      lastError: 'Tracker returned "torrent not registered"',
      errorKind: 'bad_gateway',
      downloadSpeed: 0,
      totalSize: 45_000_000_000,
    }),

    // Cancelled
    makeMockJob(3, {
      importStatus: 'download_cancelled',
      status: 'cancelled',
      progress: 0.15,
      totalSize: 4_200_000_000,
    }),

    // Awaiting import
    makeMockJob(4, {
      importStatus: 'awaiting_import',
      status: 'completed',
      progress: 1,
      totalSize: 12_000_000_000,
    }),

    // Importing (3 of 8 done)
    makeMockJob(5, {
      importStatus: 'importing',
      status: 'completed',
      progress: 1,
      totalSize: 32_000_000_000,
      totalImportTasks: 8,
      completedImports: 3,
      activeImports: 2,
      pendingImports: 3,
    }),

    // Partial failure (some imports failed)
    makeMockJob(6, {
      importStatus: 'partial_failure',
      status: 'completed',
      progress: 1,
      totalSize: 18_000_000_000,
      totalImportTasks: 5,
      completedImports: 3,
      failedImports: 2,
      lastError: 'Permission denied: /media/movies/Severance',
    }),

    // Import failed (all tasks failed)
    makeMockJob(7, {
      importStatus: 'import_failed',
      status: 'completed',
      progress: 1,
      totalSize: 6_800_000_000,
      totalImportTasks: 2,
      failedImports: 2,
      lastError: 'Destination path does not exist',
    }),

    // Fully imported
    makeMockJob(8, {
      importStatus: 'fully_imported',
      status: 'completed',
      progress: 1,
      totalSize: 22_000_000_000,
      totalImportTasks: 1,
      completedImports: 1,
    }),
  ]
}

/**
 * Simulates a download progressing from 0% through to fully imported.
 * Returns a cleanup function to clear the interval.
 */
export function simulateProgress(upsert: (job: DownloadJob) => void): () => void {
  const job = makeMockJob(3, {
    importStatus: 'download_pending',
    status: 'downloading',
    progress: 0,
    downloadSpeed: 0,
    etaSeconds: 600,
    totalSize: 10_500_000_000,
    // Use Blade Runner for the simulated one
    mediaTitle: MOCK_MEDIA[4]!.title,
    mediaPosterPath: MOCK_MEDIA[4]!.poster,
    mediaYear: MOCK_MEDIA[4]!.year,
    mediaType: MOCK_MEDIA[4]!.type,
    mediaCertification: MOCK_MEDIA[4]!.cert,
    candidateTitle: MOCK_MEDIA[4]!.candidate,
  })

  // Replace the cancelled mock that used index 3
  const totalTicks = 20
  let tick = 0
  let phase: 'downloading' | 'awaiting' | 'importing' | 'done' = 'downloading'
  let importTick = 0
  const totalImportTasks = 3

  const interval = setInterval(() => {
    tick++

    if (phase === 'downloading') {
      const progress = Math.min(tick / totalTicks, 1)
      const remaining = Math.max(0, (1 - progress) * 10)
      upsert({
        ...job,
        progress,
        downloadSpeed: 15_000_000 + Math.random() * 20_000_000,
        etaSeconds: Math.round(remaining * 60),
        updatedAt: new Date().toISOString(),
      })

      if (progress >= 1) {
        phase = 'awaiting'
      }
    } else if (phase === 'awaiting') {
      upsert({
        ...job,
        importStatus: 'awaiting_import',
        status: 'completed',
        progress: 1,
        downloadSpeed: 0,
        etaSeconds: 0,
        updatedAt: new Date().toISOString(),
      })
      phase = 'importing'
    } else if (phase === 'importing') {
      importTick++
      const completed = Math.min(importTick, totalImportTasks)
      upsert({
        ...job,
        importStatus: completed >= totalImportTasks ? 'fully_imported' : 'importing',
        status: 'completed',
        progress: 1,
        downloadSpeed: 0,
        etaSeconds: 0,
        totalImportTasks: totalImportTasks,
        completedImports: completed,
        activeImports: completed < totalImportTasks ? 1 : 0,
        pendingImports: Math.max(0, totalImportTasks - completed - 1),
        updatedAt: new Date().toISOString(),
      })

      if (completed >= totalImportTasks) {
        phase = 'done'
        clearInterval(interval)
      }
    }
  }, 500)

  return () => clearInterval(interval)
}
