// Pure zoom/fit math for the reader view. No DOM access — unit-testable in node.
export const MIN_ZOOM = 0.1;
export const ZOOM_STEP = 0.1;

function round2(z) { return Math.round(z * 100) / 100; }

// clampZoom: floor at MIN_ZOOM, no upper cap, round to 2 decimals. NaN -> 1.
export function clampZoom(z) {
  if (Number.isNaN(z)) return 1;
  if (z < MIN_ZOOM) return MIN_ZOOM;
  return round2(z);
}

// stepZoom: one ±ZOOM_STEP increment, drift-corrected and floored.
export function stepZoom(z, dir) {
  return clampZoom(round2(z) + dir * ZOOM_STEP);
}

// parsePercent: integer percent string -> fraction, floored. Unparseable -> fallback.
export function parsePercent(str, fallback) {
  const pct = parseInt(str, 10);
  if (Number.isNaN(pct)) return fallback;
  return clampZoom(pct / 100);
}

// normFit: 'width' only when explicitly 'width', else default 'height'.
export function normFit(s) {
  return s === 'width' ? 'width' : 'height';
}
