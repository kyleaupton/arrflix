// Public contract for the BottomSheet primitive — a thin, themed Vue wrapper
// around pure-web-bottom-sheet (a CSS scroll-snap web component). Callers depend
// on these types, not on the library's own, so the underlying package stays an
// implementation detail behind `@/components/ui/bottom-sheet`.

/** Sheet expansion state, mirroring pure-web's `snap-position-change` detail. */
export type BottomSheetState = 'collapsed' | 'partially-expanded' | 'expanded'

/**
 * Snap event surfaced to callers.
 * `snapIndex` 0 is the most collapsed detent; higher is more expanded.
 */
export interface BottomSheetSnapEvent {
  sheetState: BottomSheetState
  snapIndex: number
}

/** A single snap detent. */
export interface BottomSheetSnapPoint {
  /** CSS length for the detent, e.g. '100%', '50%', '320px'. */
  snap: string
  /**
   * The detent the sheet opens to. Exactly one snap should set this; if none
   * do, the sheet opens at its fully-expanded (first) detent.
   */
  initial?: boolean
}

/**
 * Imperative surface exposed via a template ref, for callers that drive the
 * sheet by ref rather than `v-model:open` (e.g. the programmatic dialog system).
 */
export interface BottomSheetHandle {
  open: () => void
  close: () => void
  snapToPoint: (index: number, behavior?: ScrollBehavior) => void
}
