// Copy checks shared by REQ tests that constrain wording rather than exact text.

const STOP = String.raw`(?:stop|stops|stopped|stopping|halt|halts|halted|abort|aborts|aborted|cancel|cancels|cancell?ed|cancell?ing|end|ends|ended)`
const WORK = String.raw`(?:download|downloads|downloading|acquisition|acquiring|search|searches|searching|grab|transfer|transfers)`

// A stop-verb and a work-noun in the same sentence, in either order, or a
// "no longer be downloaded"-style phrasing.
const PROMISES_STOP = [
  new RegExp(String.raw`\b${STOP}\b[^.?!]{0,40}\b${WORK}`, 'i'),
  new RegExp(String.raw`\b${WORK}\b[^.?!]{0,40}\b${STOP}\b`, 'i'),
  /\bno longer\b[^.?!]{0,20}\b(?:download|acquir|search|grab)/i,
]

/**
 * Whether copy tells the reader that withdrawing stops the work on a title.
 * Withdrawal removes only the viewer's own intent, so for a shared title the
 * work continues and such a promise is false (REQ-CANCEL-009).
 */
export function promisesAcquisitionStops(text: string): boolean {
  return PROMISES_STOP.some((re) => re.test(text))
}
