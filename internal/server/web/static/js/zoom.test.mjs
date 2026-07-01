import test from 'node:test';
import assert from 'node:assert/strict';
import { clampZoom, stepZoom, parsePercent, normFit, MIN_ZOOM, ZOOM_STEP } from './zoom.js';

test('constants', () => {
  assert.equal(MIN_ZOOM, 0.1);
  assert.equal(ZOOM_STEP, 0.1);
});

test('clampZoom floors at MIN_ZOOM, no upper cap, non-finite->1', () => {
  assert.equal(clampZoom(0.05), 0.1);
  assert.equal(clampZoom(0.1), 0.1);
  assert.equal(clampZoom(1), 1);
  assert.equal(clampZoom(50), 50);        // 5000%, uncapped
  assert.equal(clampZoom(NaN), 1);
  assert.equal(clampZoom(Infinity), 1);   // non-finite (e.g. hand-edited storage) -> 1
});

test('stepZoom steps by 0.1 with no float drift and floors', () => {
  assert.equal(stepZoom(1, 1), 1.1);
  assert.equal(stepZoom(1.2, -1), 1.1);   // 1.2-0.1 float drift -> rounded
  assert.equal(stepZoom(0.1, -1), 0.1);   // floored, cannot go below 0.1
});

test('parsePercent parses, floors, and falls back', () => {
  assert.equal(parsePercent('150', 1), 1.5);
  assert.equal(parsePercent('100', 1), 1);
  assert.equal(parsePercent('5', 1), 0.1);   // 5% floored to 10%
  assert.equal(parsePercent('', 1.3), 1.3);  // empty -> fallback
  assert.equal(parsePercent('abc', 1.3), 1.3);
  assert.equal(parsePercent('150abc', 1), 1.5); // parseInt stops at junk
});

test('normFit defaults to height', () => {
  assert.equal(normFit('width'), 'width');
  assert.equal(normFit('height'), 'height');
  assert.equal(normFit(null), 'height');
  assert.equal(normFit('nonsense'), 'height');
});
