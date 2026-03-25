import type { DownloadJob } from './downloadJobs'

// Real TMDB poster paths for recognizable titles
const MOCK_MEDIA = [
  { title: 'Inception', year: 2010, type: 'movie', poster: '/edv5CZvWj09upOy2maq0Qs58RDI.jpg', cert: 'PG-13', candidate: 'Inception.2010.2160p.UHD.BluRay.x265-GROUP' },
  { title: 'The Dark Knight', year: 2008, type: 'movie', poster: '/qJ2tW6WMUDux911BTUgMe1nREA.jpg', cert: 'PG-13', candidate: 'The.Dark.Knight.2008.1080p.BluRay.x264-GROUP' },
  { title: 'Interstellar', year: 2014, type: 'movie', poster: '/gEU2QniE6E77NI6lCU6MxlNBvIx.jpg', cert: 'PG-13', candidate: 'Interstellar.2014.2160p.UHD.BluRay.Remux.HDR.HEVC.DTS-HD.MA-GROUP' },
  { title: 'Breaking Bad', year: 2008, type: 'series', poster: '/ztkUQFLlC19CCMYHW9o1zp7XMAK.jpg', cert: 'TV-MA', candidate: 'Breaking.Bad.S05E16.Felina.1080p.BluRay.x264-GROUP', season: 5, episode: 16 },
  { title: 'Blade Runner 2049', year: 2017, type: 'movie', poster: '/gajva2L0rPYkEWjzgFlBXCAVBE5.jpg', cert: 'R', candidate: 'Blade.Runner.2049.2017.1080p.BluRay.x264-GROUP' },
  { title: 'Dune', year: 2021, type: 'movie', poster: '/d5NXSklXo0qyIYkgV94XAgMIckC.jpg', cert: 'PG-13', candidate: 'Dune.2021.2160p.WEB-DL.DDP5.1.Atmos.HDR.H.265-GROUP' },
  { title: 'Severance', year: 2022, type: 'series', poster: '/lFGgqFhRCoCjMlk1sVuIFDzflsu.jpg', cert: 'TV-MA', candidate: 'Severance.S01E09.The.We.We.Are.1080p.ATVP.WEB-DL.DDP5.1.H.264-GROUP', season: 1, episode: 9 },
  { title: 'The Matrix', year: 1999, type: 'movie', poster: '/f89U3ADr1oiB1s9GkdPOEpXUk5H.jpg', cert: 'R', candidate: 'The.Matrix.1999.2160p.UHD.BluRay.Remux.HDR.HEVC.DTS-HD.MA-GROUP' },
  { title: 'Chernobyl', year: 2019, type: 'series', poster: '/hlLXt2tOPT6RRnjiUmoxyG1LTFi.jpg', cert: 'TV-MA', candidate: 'Chernobyl.S01E05.Vichnaya.Pamyat.1080p.AMZN.WEB-DL.DDP5.1.H.264-GROUP', season: 1, episode: 5 },
] as const

function makeMockJob(
  index: number,
  overrides: Partial<DownloadJob>,
): DownloadJob {
  const media = MOCK_MEDIA[index % MOCK_MEDIA.length]
  const now = new Date()
  const updatedAt = new Date(now.getTime() - index * 60_000) // stagger by 1 min each

  return {
    id: crypto.randomUUID(),
    active_imports: 0,
    attempt_count: 1,
    cancelled_imports: 0,
    candidate_link: '',
    candidate_title: media.candidate,
    completed_imports: 0,
    content_path: `/downloads/${media.candidate}`,
    created_at: new Date(updatedAt.getTime() - 600_000).toISOString(),
    download_speed: 0,
    downloader_external_id: '',
    downloader_id: crypto.randomUUID(),
    downloader_status: '',
    episode_id: media.type === 'series' ? crypto.randomUUID() : '',
    episode_number: media.type === 'series' ? (media.episode ?? 0) : 0,
    error_category: '',
    eta_seconds: 0,
    failed_imports: 0,
    guid: crypto.randomUUID(),
    import_status: 'download_pending',
    indexer_id: 1,
    last_error: '',
    library_id: crypto.randomUUID(),
    media_certification: media.cert,
    media_item_id: crypto.randomUUID(),
    media_poster_path: media.poster,
    media_title: media.title,
    media_type: media.type,
    media_year: media.year,
    name_template_id: crypto.randomUUID(),
    next_run_at: '',
    pending_imports: 0,
    previous_job_id: '',
    progress: 0,
    protocol: 'torrent',
    save_path: '/downloads',
    season_id: media.type === 'series' ? crypto.randomUUID() : '',
    season_number: media.type === 'series' ? (media.season ?? 0) : 0,
    status: 'downloading',
    tmdb_id: 1000 + index,
    total_import_tasks: 0,
    total_size: 0,
    updated_at: updatedAt.toISOString(),
    ...overrides,
  }
}

export function createMockJobs(): DownloadJob[] {
  return [
    // Active: just started (0%)
    makeMockJob(0, {
      import_status: 'download_pending',
      status: 'downloading',
      progress: 0,
      download_speed: 0,
      eta_seconds: 0,
      total_size: 15_000_000_000,
    }),

    // Active: mid-download (45%)
    makeMockJob(1, {
      import_status: 'download_pending',
      status: 'downloading',
      progress: 0.45,
      download_speed: 25_000_000,
      eta_seconds: 330,
      total_size: 8_500_000_000,
    }),

    // Failed download
    makeMockJob(2, {
      import_status: 'download_failed',
      status: 'failed',
      progress: 0.72,
      last_error: 'Tracker returned "torrent not registered"',
      error_category: 'tracker_error',
      download_speed: 0,
      total_size: 45_000_000_000,
    }),

    // Cancelled
    makeMockJob(3, {
      import_status: 'download_cancelled',
      status: 'cancelled',
      progress: 0.15,
      total_size: 4_200_000_000,
    }),

    // Awaiting import
    makeMockJob(4, {
      import_status: 'awaiting_import',
      status: 'completed',
      progress: 1,
      total_size: 12_000_000_000,
    }),

    // Importing (3 of 8 done)
    makeMockJob(5, {
      import_status: 'importing',
      status: 'completed',
      progress: 1,
      total_size: 32_000_000_000,
      total_import_tasks: 8,
      completed_imports: 3,
      active_imports: 2,
      pending_imports: 3,
    }),

    // Partial failure (some imports failed)
    makeMockJob(6, {
      import_status: 'partial_failure',
      status: 'completed',
      progress: 1,
      total_size: 18_000_000_000,
      total_import_tasks: 5,
      completed_imports: 3,
      failed_imports: 2,
      last_error: 'Permission denied: /media/movies/Severance',
    }),

    // Import failed (all tasks failed)
    makeMockJob(7, {
      import_status: 'import_failed',
      status: 'completed',
      progress: 1,
      total_size: 6_800_000_000,
      total_import_tasks: 2,
      failed_imports: 2,
      last_error: 'Destination path does not exist',
    }),

    // Fully imported
    makeMockJob(8, {
      import_status: 'fully_imported',
      status: 'completed',
      progress: 1,
      total_size: 22_000_000_000,
      total_import_tasks: 1,
      completed_imports: 1,
    }),
  ]
}

/**
 * Simulates a download progressing from 0% through to fully imported.
 * Returns a cleanup function to clear the interval.
 */
export function simulateProgress(upsert: (job: DownloadJob) => void): () => void {
  const job = makeMockJob(3, {
    import_status: 'download_pending',
    status: 'downloading',
    progress: 0,
    download_speed: 0,
    eta_seconds: 600,
    total_size: 10_500_000_000,
    // Use Blade Runner for the simulated one
    media_title: MOCK_MEDIA[4].title,
    media_poster_path: MOCK_MEDIA[4].poster,
    media_year: MOCK_MEDIA[4].year,
    media_type: MOCK_MEDIA[4].type,
    media_certification: MOCK_MEDIA[4].cert,
    candidate_title: MOCK_MEDIA[4].candidate,
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
        download_speed: 15_000_000 + Math.random() * 20_000_000,
        eta_seconds: Math.round(remaining * 60),
        updated_at: new Date().toISOString(),
      })

      if (progress >= 1) {
        phase = 'awaiting'
      }
    } else if (phase === 'awaiting') {
      upsert({
        ...job,
        import_status: 'awaiting_import',
        status: 'completed',
        progress: 1,
        download_speed: 0,
        eta_seconds: 0,
        updated_at: new Date().toISOString(),
      })
      phase = 'importing'
    } else if (phase === 'importing') {
      importTick++
      const completed = Math.min(importTick, totalImportTasks)
      upsert({
        ...job,
        import_status: completed >= totalImportTasks ? 'fully_imported' : 'importing',
        status: 'completed',
        progress: 1,
        download_speed: 0,
        eta_seconds: 0,
        total_import_tasks: totalImportTasks,
        completed_imports: completed,
        active_imports: completed < totalImportTasks ? 1 : 0,
        pending_imports: Math.max(0, totalImportTasks - completed - 1),
        updated_at: new Date().toISOString(),
      })

      if (completed >= totalImportTasks) {
        phase = 'done'
        clearInterval(interval)
      }
    }
  }, 500)

  return () => clearInterval(interval)
}
