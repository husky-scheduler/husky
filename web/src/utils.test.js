/**
 * Utility logic tests — pure functions replicated here so they can be tested
 * without importing the full Preact component tree.
 */
import { describe, it, expect } from 'vitest'

// ── goDurSec ─────────────────────────────────────────────────────────────────
// Parses a Go duration string like "2h30m5s" → total seconds.
function goDurSec(s) {
  if (!s) return 0
  let total = 0
  const re = /(\d+)(h|m|s)/g
  let m
  while ((m = re.exec(s)) !== null)
    total += parseInt(m[1]) * { h: 3600, m: 60, s: 1 }[m[2]]
  return total
}

describe('goDurSec', () => {
  it('returns 0 for empty string', () => expect(goDurSec('')).toBe(0))
  it('returns 0 for null', ()        => expect(goDurSec(null)).toBe(0))
  it('parses plain seconds',         () => expect(goDurSec('30s')).toBe(30))
  it('parses plain minutes',         () => expect(goDurSec('5m')).toBe(300))
  it('parses plain hours',           () => expect(goDurSec('2h')).toBe(7200))
  it('parses compound h+m+s',        () => expect(goDurSec('1h30m10s')).toBe(5410))
  it('parses compound m+s',          () => expect(goDurSec('2m45s')).toBe(165))
  it('ignores unknown units',        () => expect(goDurSec('5x')).toBe(0))
})

// ── fmtUptime ────────────────────────────────────────────────────────────────
function fmtUptime(s) {
  const sec = goDurSec(s)
  if (sec < 60)   return `${sec}s`
  if (sec < 3600) return `${Math.floor(sec / 60)}m`
  const h = Math.floor(sec / 3600), min = Math.floor((sec % 3600) / 60)
  return min ? `${h}h ${min}m` : `${h}h`
}

describe('fmtUptime', () => {
  it('formats sub-minute as seconds',    () => expect(fmtUptime('45s')).toBe('45s'))
  it('formats exact minute',             () => expect(fmtUptime('60s')).toBe('1m'))
  it('formats sub-hour as minutes',      () => expect(fmtUptime('90s')).toBe('1m'))
  it('formats exact hour with no mins',  () => expect(fmtUptime('1h')).toBe('1h'))
  it('formats hour + minutes',           () => expect(fmtUptime('1h30m')).toBe('1h 30m'))
  it('formats multiple hours',           () => expect(fmtUptime('3h15m')).toBe('3h 15m'))
})

// ── diffLines ────────────────────────────────────────────────────────────────
// Myers-style LCS-based diff: returns [{type:'same'|'add'|'remove', line}]
function diffLines(oldTxt, newTxt) {
  const a = oldTxt.split('\n')
  const b = newTxt.split('\n')
  const m = a.length, n = b.length
  const dp = Array.from({ length: m + 1 }, () => new Array(n + 1).fill(0))
  for (let i = m - 1; i >= 0; i--)
    for (let j = n - 1; j >= 0; j--)
      dp[i][j] = a[i] === b[j] ? dp[i + 1][j + 1] + 1 : Math.max(dp[i + 1][j], dp[i][j + 1])
  const result = []
  let i = 0, j = 0
  while (i < m || j < n) {
    if (i < m && j < n && a[i] === b[j]) { result.push({ type: 'same',   line: a[i] }); i++; j++ }
    else if (j < n && (i >= m || dp[i][j + 1] >= dp[i + 1][j])) { result.push({ type: 'add',    line: b[j] }); j++ }
    else { result.push({ type: 'remove', line: a[i] }); i++ }
  }
  return result
}

describe('diffLines', () => {
  it('returns all "same" for identical text', () => {
    const d = diffLines('a\nb\nc', 'a\nb\nc')
    expect(d.every(x => x.type === 'same')).toBe(true)
    expect(d).toHaveLength(3)
  })

  it('detects an added line', () => {
    const d = diffLines('a\nb', 'a\nb\nc')
    expect(d.some(x => x.type === 'add' && x.line === 'c')).toBe(true)
  })

  it('detects a removed line', () => {
    const d = diffLines('a\nb\nc', 'a\nc')
    expect(d.some(x => x.type === 'remove' && x.line === 'b')).toBe(true)
  })

  it('handles empty old text', () => {
    const d = diffLines('', 'hello')
    // 'hello' is new content → at least one 'add' entry; nothing is 'same'
    expect(d.some(x => x.type === 'add' && x.line === 'hello')).toBe(true)
    expect(d.some(x => x.type === 'same')).toBe(false)
  })

  it('handles empty new text', () => {
    const d = diffLines('hello', '')
    // 'hello' was removed → at least one 'remove' entry; nothing is 'same'
    expect(d.some(x => x.type === 'remove' && x.line === 'hello')).toBe(true)
    expect(d.some(x => x.type === 'same')).toBe(false)
  })

  it('preserves all lines in the result', () => {
    const d = diffLines('a\nb\nc', 'a\nx\nc')
    const allLines = d.map(x => x.line)
    expect(allLines).toContain('a')
    expect(allLines).toContain('b')
    expect(allLines).toContain('x')
    expect(allLines).toContain('c')
  })
})

// ── fmtRel ───────────────────────────────────────────────────────────────────
function fmtRel(iso) {
  if (!iso) return '—'
  const d = Math.floor((Date.now() - new Date(iso).getTime()) / 1000)
  if (d < 5)     return 'just now'
  if (d < 60)    return `${d}s ago`
  if (d < 3600)  return `${Math.floor(d / 60)}m ago`
  if (d < 86400) return `${Math.floor(d / 3600)}h ago`
  return `${Math.floor(d / 86400)}d ago`
}

describe('fmtRel', () => {
  it('returns — for null',          () => expect(fmtRel(null)).toBe('—'))
  it('returns "just now" within 5s', () => {
    const ts = new Date(Date.now() - 2000).toISOString()
    expect(fmtRel(ts)).toBe('just now')
  })
  it('returns seconds ago', () => {
    const ts = new Date(Date.now() - 30_000).toISOString()
    expect(fmtRel(ts)).toBe('30s ago')
  })
  it('returns minutes ago', () => {
    const ts = new Date(Date.now() - 5 * 60_000).toISOString()
    expect(fmtRel(ts)).toBe('5m ago')
  })
  it('returns hours ago', () => {
    const ts = new Date(Date.now() - 3 * 3_600_000).toISOString()
    expect(fmtRel(ts)).toBe('3h ago')
  })
  it('returns days ago', () => {
    const ts = new Date(Date.now() - 2 * 86_400_000).toISOString()
    expect(fmtRel(ts)).toBe('2d ago')
  })
})
