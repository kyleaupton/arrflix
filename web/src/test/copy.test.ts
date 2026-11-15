import { describe, expect, test } from 'vitest'
import { promisesAcquisitionStops } from './copy'

describe('promisesAcquisitionStops', () => {
  test.each([
    'This will stop the download.',
    'Cancel the download for Dune Part Two?',
    'Arrflix will stop searching for this title.',
    'The transfer will be aborted.',
    'It will no longer be downloaded.',
    'Acquisition ends when you withdraw.',
  ])('flags %j', (text) => {
    expect(promisesAcquisitionStops(text)).toBe(true)
  })

  test.each([
    // The confirmation story 7 holds up as honest.
    'Remove your request for Dune Part Two? It may remain in the library if someone else requested it.',
    'Withdraw this request?',
    'Cancel request',
    'Withdraw request',
  ])('passes %j', (text) => {
    expect(promisesAcquisitionStops(text)).toBe(false)
  })
})
