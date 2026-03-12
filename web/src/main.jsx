import { h, render } from 'preact'
import { useState, useEffect, useCallback, useRef, useMemo } from 'preact/hooks'
import { 
  PlayIcon, PauseIcon, XIcon, ReloadIcon, StopIcon, RefreshCwIcon, ActivityIcon, AlertIcon, 
  CheckIcon, EditIcon, SaveIcon, ChevronRightSolidIcon, ChevronDownSolidIcon 
} from './icons'
import './index.css'

// ─── Status colour map ───────────────────────────────────────────────────────
const STATUS_CLS = {
  RUNNING:    'bg-blue-100   text-blue-800   ring-blue-600/20',
  SUCCESS:    'bg-green-100  text-green-800  ring-green-600/20',
  FAILED:     'bg-red-100    text-red-800    ring-red-600/20',
  CANCELLED:  'bg-gray-100   text-gray-600   ring-gray-500/20',
  SKIPPED:    'bg-yellow-100 text-yellow-700 ring-yellow-600/20',
  PENDING:    'bg-purple-100 text-purple-800 ring-purple-600/20',
  SLA_BREACH: 'bg-amber-100  text-amber-800  ring-amber-600/20',
  PAUSED:     'bg-orange-100 text-orange-700 ring-orange-600/20',
}

// ─── Helpers ─────────────────────────────────────────────────────────────────
function fmtRel(iso) {
  if (!iso) return '—'
  const d = Math.floor((Date.now() - new Date(iso).getTime()) / 1000)
  if (d < 5)     return 'just now'
  if (d < 60)    return `${d}s ago`
  if (d < 3600)  return `${Math.floor(d / 60)}m ago`
  if (d < 86400) return `${Math.floor(d / 3600)}h ago`
  return `${Math.floor(d / 86400)}d ago`
}

function fmtTs(iso) {
  if (!iso) return '—'
  return new Date(iso).toLocaleString()
}

function durSec(start, end) {
  if (!start || !end) return null
  return Math.round((new Date(end) - new Date(start)) / 1000)
}

// Parse a Go duration string like "2h30m5s" → seconds
function goDurSec(s) {
  if (!s) return 0
  let total = 0
  const re = /(\d+)(h|m|s)/g; let m
  while ((m = re.exec(s)) !== null)
    total += parseInt(m[1]) * { h: 3600, m: 60, s: 1 }[m[2]]
  return total
}

function fmtUptime(s) {
  const sec = goDurSec(s)
  if (sec < 60)   return `${sec}s`
  if (sec < 3600) return `${Math.floor(sec / 60)}m`
  const h = Math.floor(sec / 3600), min = Math.floor((sec % 3600) / 60)
  return min ? `${h}h ${min}m` : `${h}h`
}

// Format a future ISO timestamp as "in 2h", "in 30m", "in 3d", etc.
function fmtNext(iso) {
  if (!iso) return '—'
  const d = Math.floor((new Date(iso).getTime() - Date.now()) / 1000)
  if (d <= 0)         return 'now'
  if (d < 60)         return `in ${d}s`
  if (d < 3600)       return `in ${Math.floor(d / 60)}m`
  if (d < 86400)      return `in ${Math.floor(d / 3600)}h`
  return `in ${Math.floor(d / 86400)}d`
}

// Derive last-run info from a job summary row
function jobLastRun(job) {
  if (job.running) return { ts: null, status: 'RUNNING' }
  if (!job.last_success && !job.last_failure) return { ts: null, status: null }
  const s = job.last_success ? new Date(job.last_success) : null
  const f = job.last_failure ? new Date(job.last_failure) : null
  if (s && (!f || s > f)) return { ts: job.last_success, status: 'SUCCESS' }
  if (f && (!s || f > s)) return { ts: job.last_failure,  status: 'FAILED'  }
  return { ts: null, status: null }
}

async function apiPost(path, body = null) {
  return fetch(path, {
    method: 'POST',
    headers: body ? { 'Content-Type': 'application/json' } : {},
    body:    body ? JSON.stringify(body) : undefined,
  })
}

// ─── Toast ────────────────────────────────────────────────────────────────────
function Toast({ msg, type, onClose }) {
  useEffect(() => {
    const id = setTimeout(onClose, 3000)
    return () => clearTimeout(id)
  }, [msg])
  const bg = type === 'error' ? 'bg-red-600' : 'bg-gray-800'
  return (
    <div class={`fixed bottom-5 right-5 z-50 flex items-center gap-3 rounded-lg px-4 py-3 text-sm text-white shadow-lg ${bg}`}>
      <span>{msg}</span>
      <button onClick={onClose} class="opacity-70 hover:opacity-100 flex items-center justify-center p-1 rounded-full"><XIcon className="w-4 h-4" /></button>
    </div>
  )
}

function useToast() {
  const [toast, setToast] = useState(null)
  const show = (msg, type = 'ok') => setToast({ msg, type })
  const hide = () => setToast(null)
  return { toast, show, hide }
}

// ─── useApi hook ─────────────────────────────────────────────────────────────
function useApi(path, intervalMs = 0) {
  const [data,    setData]    = useState(null)
  const [error,   setError]   = useState(null)
  const [loading, setLoading] = useState(true)

  const load = useCallback(async () => {
    if (!path) return
    try {
      const res = await fetch(path)
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      setData(await res.json())
      setError(null)
    } catch (e) { setError(e.message) }
    finally     { setLoading(false)   }
  }, [path])

  useEffect(() => {
    setLoading(true)
    load()
    if (intervalMs > 0) {
      const id = setInterval(load, intervalMs)
      return () => clearInterval(id)
    }
  }, [load, intervalMs])

  return { data, error, loading, reload: load }
}

// ─── Shared components ───────────────────────────────────────────────────────
function Badge({ status }) {
  const cls = STATUS_CLS[status] ?? 'bg-gray-100 text-gray-600 ring-gray-500/20'
  return (
    <span class={`inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium ring-1 ring-inset ${cls}`}>
      {status}
    </span>
  )
}

function Spinner({ sm }) {
  const sz = sm ? 'h-3 w-3' : 'h-4 w-4'
  return (
    <svg class={`animate-spin ${sz} text-blue-500`} viewBox="0 0 24 24" fill="none">
      <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4" />
      <path  class="opacity-75"  fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
    </svg>
  )
}

function Th({ children, right }) {
  return (
    <th class={`px-4 py-3 text-xs font-semibold uppercase tracking-wider text-gray-500 ${right ? 'text-right' : 'text-left'}`}>
      {children}
    </th>
  )
}

function EmptyRow({ cols, loading, msg }) {
  return (
    <tr>
      <td colspan={cols} class="px-4 py-10 text-center text-sm text-gray-400">
        {loading ? <span class="flex items-center justify-center gap-2"><Spinner />Loading…</span> : msg}
      </td>
    </tr>
  )
}

function FInput({ label, value, onChange, placeholder, type = 'text' }) {
  return (
    <label class="flex flex-col gap-1">
      <span class="text-xs font-medium text-gray-500">{label}</span>
      <input
        type={type}
        value={value}
        onInput={e => onChange(e.target.value)}
        placeholder={placeholder}
        class="rounded-md border border-gray-300 bg-white px-2.5 py-1.5 text-sm text-gray-700 shadow-sm placeholder:text-gray-300 focus:border-blue-500 focus:outline-none w-36"
      />
    </label>
  )
}

function FSel({ label, value, onChange, opts }) {
  return (
    <label class="flex flex-col gap-1">
      <span class="text-xs font-medium text-gray-500">{label}</span>
      <select
        value={value}
        onChange={e => onChange(e.target.value)}
        class="rounded-md border border-gray-300 bg-white px-2.5 py-1.5 text-sm text-gray-700 shadow-sm focus:border-blue-500 focus:outline-none"
      >
        {opts.map(o => <option key={o} value={o}>{o || 'Any'}</option>)}
      </select>
    </label>
  )
}

// Job name select — fetches /api/jobs once for the dropdown options
function FJobSel({ label = 'Job', value, onChange, jobNames }) {
  return (
    <label class="flex flex-col gap-1">
      <span class="text-xs font-medium text-gray-500">{label}</span>
      <select
        value={value}
        onChange={e => onChange(e.target.value)}
        class="rounded-md border border-gray-300 bg-white px-2.5 py-1.5 text-sm text-gray-700 shadow-sm focus:border-blue-500 focus:outline-none"
      >
        <option value="">All jobs</option>
        {(jobNames ?? []).map(n => <option key={n} value={n}>{n}</option>)}
      </select>
    </label>
  )
}

// ─── Tag health strip ────────────────────────────────────────────────────────
function TagHealthStrip({ jobs, tagFilter, setTagFilter, showToast }) {
  const [confirmTag, setConfirmTag] = useState(null)  // tag name pending Run All

  const tagStats = {}
  for (const job of jobs ?? []) {
    for (const tag of job.tags ?? []) {
      if (!tagStats[tag]) tagStats[tag] = { running: 0, ok: 0, failed: 0, jobs: [] }
      tagStats[tag].jobs.push(job.name)
      if (job.running)                        tagStats[tag].running++
      else if (jobLastRun(job).status === 'SUCCESS') tagStats[tag].ok++
      else if (jobLastRun(job).status === 'FAILED')  tagStats[tag].failed++
    }
  }
  const tags = Object.keys(tagStats).sort()
  if (tags.length === 0) return null

  async function runAll(tag) {
    const names = tagStats[tag]?.jobs ?? []
    const results = await Promise.all(names.map(n => apiPost(`/api/jobs/${n}/run`)))
    const ok = results.filter(r => r.ok).length
    const fail = results.length - ok
    showToast(
      fail === 0 ? `Triggered ${ok} jobs in tag "${tag}"` : `${ok} triggered, ${fail} failed`,
      fail === 0 ? 'ok' : 'error'
    )
    setConfirmTag(null)
  }

  return (
    <div class="flex flex-wrap gap-2 px-5 pt-4">
      {tags.map(tag => {
        const s = tagStats[tag]
        const isActive = tagFilter === tag
        return (
          <div key={tag} class="flex items-stretch rounded-lg border overflow-hidden transition-colors"
            style={isActive ? 'border-color:#93c5fd' : 'border-color:#e5e7eb'}>
            <button
              onClick={() => setTagFilter(tagFilter === tag ? '' : tag)}
              class={[
                'flex items-center gap-2 px-3 py-1.5 text-xs font-medium transition-colors',
                isActive ? 'bg-blue-50 text-blue-700' : 'bg-white text-gray-600 hover:bg-gray-50',
              ].join(' ')}
            >
              <span class="font-mono">{tag}</span>
              {s.running > 0 && (
                <span class="flex items-center gap-1 text-blue-600">
                  <span class="h-1.5 w-1.5 rounded-full bg-blue-500 animate-pulse" />{s.running}
                </span>
              )}
              {s.ok > 0 && <span class="text-green-600 font-medium flex items-center gap-1"><CheckIcon className="w-3 h-3" /> {s.ok}</span>}
              {s.failed > 0 && <span class="text-red-600 font-medium flex items-center gap-1"><XIcon className="w-3 h-3" /> {s.failed}</span>}
            </button>
            <button
              onClick={() => setConfirmTag(tag)}
              class="flex items-center px-2 bg-gray-50 text-gray-400 hover:bg-blue-50 hover:text-blue-600 border-l border-gray-200 text-xs transition-colors"
              title={`Run all jobs in tag "${tag}"`}
            >
              <PlayIcon className="w-3.5 h-3.5" />
            </button>
          </div>
        )
      })}

      {/* Run All confirmation modal */}
      {confirmTag && (
        <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/30" onClick={() => setConfirmTag(null)}>
          <div class="bg-white rounded-xl border border-gray-200 shadow-xl p-5 max-w-sm w-full mx-4" onClick={e => e.stopPropagation()}>
            <h3 class="text-sm font-semibold text-gray-800 mb-2">Run all jobs in tag <span class="font-mono text-blue-700">{confirmTag}</span>?</h3>
            <p class="text-xs text-gray-500 mb-3">This will trigger the following jobs immediately:</p>
            <ul class="text-xs font-mono text-gray-700 bg-gray-50 rounded p-2 mb-4 max-h-40 overflow-y-auto space-y-0.5">
              {(tagStats[confirmTag]?.jobs ?? []).map(n => <li key={n}>{n}</li>)}
            </ul>
            <div class="flex justify-end gap-2">
              <button onClick={() => setConfirmTag(null)} class="rounded border border-gray-300 bg-white px-3 py-1.5 text-xs font-medium text-gray-700 hover:bg-gray-50">Cancel</button>
              <button onClick={() => runAll(confirmTag)} class="rounded border border-blue-300 bg-blue-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-blue-700">Run All</button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

// ─── Daemon Info Panel ───────────────────────────────────────────────────────
function DaemonInfoPanel({ onClose }) {
  const { data, loading } = useApi('/api/daemon/info', 30000)

  const rows = data ? [
    { label: 'Version',     value: data.version || '—' },
    { label: 'PID',         value: data.pid ?? '—' },
    { label: 'Uptime',      value: data.uptime || '—' },
    { label: 'Started at',  value: data.started_at ? fmtTs(data.started_at) : '—' },
    { label: 'Config file', value: data.config_path || '—', mono: true },
    { label: 'DB path',     value: data.db_path || '—', mono: true },
    { label: 'Jobs total',  value: data.total_job_count ?? '—' },
    { label: 'Running',     value: data.active_job_count ?? '—' },
    { label: 'Paused',      value: data.paused_job_count ?? '—' },
  ] : []

  return (
    <div class="absolute top-full right-0 z-50 mt-2 w-80 rounded-xl border border-gray-200 bg-white shadow-xl text-xs">
      <div class="flex items-center justify-between px-4 py-2.5 border-b border-gray-100">
        <span class="text-xs font-semibold text-gray-700">Daemon Info</span>
        <button onClick={onClose} class="text-gray-400 hover:text-gray-600 p-0.5 rounded">
          <XIcon className="w-4 h-4" />
        </button>
      </div>
      <div class="px-4 py-3">
        {loading ? (
          <div class="flex items-center gap-2 text-gray-400"><Spinner sm />Loading…</div>
        ) : data ? (
          <dl class="space-y-1.5">
            {rows.map(({ label, value, mono }) => (
              <div key={label} class="flex items-start justify-between gap-3">
                <dt class="text-gray-500 font-medium shrink-0">{label}</dt>
                <dd class={`text-gray-800 text-right break-all ${mono ? 'font-mono' : ''}`}>{value}</dd>
              </div>
            ))}
          </dl>
        ) : (
          <p class="text-gray-400 italic">Daemon offline</p>
        )}
      </div>
    </div>
  )
}

// ─── TopBar ──────────────────────────────────────────────────────────────────
function TopBar({ status, onStop, onReload }) {
  const [confirming, setConfirming] = useState(false)
  const [showInfo,   setShowInfo]   = useState(false)
  const alive = !!status
  const upStr = fmtUptime(status?.uptime)
  return (
    <header class="flex items-center gap-4 px-6 py-4 bg-white border-b border-gray-200 shadow-sm relative">
      <span class="text-xl font-bold tracking-tight text-gray-900 flex items-center">
        <img src="/husky_logo_nobg.png" alt="Husky" class="h-8 w-12" />
        <span>Husky</span>
      </span>
      <div class={`flex items-center gap-1.5 text-sm font-medium ${alive ? 'text-green-600' : 'text-gray-400'}`}>
        <span class={`h-2.5 w-2.5 rounded-full ${alive ? 'bg-green-500 animate-pulse' : 'bg-gray-300'}`} />
        {alive ? 'huskyd running' : 'connecting…'}
      </div>
      {upStr && <span class="text-xs text-gray-400 font-medium">up {upStr}</span>}
      <div class="ml-auto flex items-center gap-3">
        {status?.version && (
          <div class="relative">
            <button
              onClick={() => setShowInfo(v => !v)}
              class={`rounded-full px-3 py-1 text-xs font-mono border transition-colors ${showInfo ? 'bg-blue-50 border-blue-300 text-blue-700' : 'bg-gray-100 border-gray-200 text-gray-600 hover:bg-gray-200'}`}
              title="Daemon info"
            >
              v{status.version}
            </button>
            {showInfo && <DaemonInfoPanel onClose={() => setShowInfo(false)} />}
          </div>
        )}
        {alive && (
          <>
            <button
              onClick={onReload}
              class="flex items-center gap-2 rounded-md border border-gray-300 bg-white px-3 py-2 text-xs font-medium text-gray-700 shadow-sm hover:bg-gray-50 transition-colors"
              title="Hot-reload husky.yaml without stopping the daemon"
            >
              <ReloadIcon className="w-3.5 h-3.5" /> Reload Config
            </button>
            {!confirming ? (
              <button
                onClick={() => setConfirming(true)}
                class="flex items-center gap-2 rounded-md border border-red-200 bg-red-50 px-3 py-2 text-xs font-medium text-red-700 shadow-sm hover:bg-red-100 transition-colors"
              >
                <StopIcon className="w-3.5 h-3.5" /> Stop Daemon
              </button>
            ) : (
              <span class="flex items-center gap-2">
                <span class="text-xs text-gray-500 font-medium tracking-wide uppercase">Confirm stop?</span>
                <button
                  onClick={() => { setConfirming(false); onStop(false) }}
                  class="rounded-md border border-red-300 bg-white px-3 py-1.5 text-xs font-medium text-red-700 hover:bg-red-50 transition-colors shadow-sm"
                >
                  Graceful
                </button>
                <button
                  onClick={() => { setConfirming(false); onStop(true) }}
                  class="rounded-md border border-red-600 bg-red-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-red-700 transition-colors shadow-sm"
                >
                  Force
                </button>
                <button onClick={() => setConfirming(false)} class="text-gray-400 hover:text-gray-600 p-1 flex items-center justify-center rounded-full hover:bg-gray-100">
                  <XIcon className="w-4 h-4" />
                </button>
              </span>
            )}
          </>
        )}
      </div>
    </header>
  )
}

// ─── Tabs ────────────────────────────────────────────────────────────────────
const TAB_LIST = ['Jobs', 'Audit', 'Outputs', 'Data', 'Alerts', 'DAG', 'Health', 'Integrations', 'Config']

function Tabs({ active, onChange }) {
  return (
    <nav class="flex gap-0 px-6 bg-white border-b border-gray-200">
      {TAB_LIST.map(t => (
        <button
          key={t}
          onClick={() => onChange(t)}
          class={[
            'px-5 py-3 text-sm font-medium border-b-2 transition-colors',
            active === t
              ? 'border-blue-600 text-blue-600'
              : 'border-transparent text-gray-500 hover:text-gray-800 hover:border-gray-300',
          ].join(' ')}
        >
          {t}
        </button>
      ))}
    </nav>
  )
}

// ─── Log Viewer (WS + historic) ──────────────────────────────────────────────
function LogViewer({ runId, live }) {
  const [lines,    setLines]    = useState([])
  const [wsState,  setWsState]  = useState('idle')
  const [showHC,   setShowHC]   = useState(false)
  const bottomRef              = useRef(null)

  useEffect(() => {
    if (!runId) return
    setLines([])
    setWsState('connecting')
    const hcParam = showHC ? '?include_healthcheck=true' : ''

    if (live) {
      const proto = location.protocol === 'https:' ? 'wss:' : 'ws:'
      const ws    = new WebSocket(`${proto}//${location.host}/ws/logs/${runId}${hcParam}`)
      ws.onopen    = () => setWsState('open')
      ws.onclose   = () => setWsState('closed')
      ws.onerror   = () => setWsState('error')
      ws.onmessage = ({ data }) => {
        try {
          const msg = JSON.parse(data)
          if (msg.type === 'log') setLines(prev => [...prev, msg])
          if (msg.type === 'end') setWsState('ended')
        } catch { /* ignore malformed */ }
      }
      return () => ws.close()
    } else {
      fetch(`/api/runs/${runId}/logs${hcParam}`)
        .then(r => r.json())
        .then(arr => { setLines(arr ?? []); setWsState('done') })
        .catch(() => setWsState('error'))
    }
  }, [runId, live, showHC])

  useEffect(() => { bottomRef.current?.scrollIntoView({ behavior: 'smooth' }) }, [lines])

  const streamBadge = {
    open:       <span class="text-green-400 flex items-center gap-1"><span class="h-2 w-2 rounded-full bg-green-400 animate-pulse"></span> live</span>,
    ended:      <span class="text-gray-400 flex items-center gap-1"><span class="h-2 w-2 rounded-sm bg-gray-400"></span> ended</span>,
    closed:     <span class="text-gray-500 flex items-center gap-1"><span class="h-2 w-2 rounded-sm bg-gray-500"></span> disconnected</span>,
    error:      <span class="text-red-400 flex items-center gap-1"><AlertIcon className="w-3 h-3" /> error</span>,
    done:       <span class="text-gray-400 flex items-center gap-1">historic</span>,
    connecting: <span class="text-yellow-400 flex items-center gap-1">connecting…</span>,
  }[wsState] ?? null

  return (
    <div class="rounded-lg border border-gray-700 overflow-hidden bg-gray-950">
      <div class="flex items-center justify-between px-3 py-1.5 bg-gray-900 border-b border-gray-700">
        <span class="text-xs font-mono text-gray-400">run #{runId}</span>
        <div class="flex items-center gap-3">
          <button
            onClick={() => setShowHC(v => !v)}
            class={`flex items-center gap-1.5 text-xs font-mono transition-colors ${showHC ? 'text-amber-400' : 'text-gray-500 hover:text-gray-300'}`}
            title="Toggle healthcheck log lines"
          >
            <ActivityIcon className="w-3.5 h-3.5" />
            {showHC ? 'HC: ON' : 'HC: OFF'}
          </button>
          <span class="text-xs font-mono flex items-center">{streamBadge}</span>
        </div>
      </div>
      <pre class="text-xs font-mono text-gray-200 p-3 max-h-72 overflow-y-auto leading-relaxed whitespace-pre-wrap">
        {lines.length === 0
          ? <span class="text-gray-600">No output yet…</span>
          : lines.map((l, i) => (
              <div key={i} class={
                l.stream === 'stderr'       ? 'text-red-300'
                : l.stream === 'healthcheck' ? 'text-amber-300'
                : ''
              }>
                <span class="text-gray-600 select-none mr-2">{l.ts?.slice(11, 19)}</span>
                {l.line}
              </div>
            ))
        }
        <div ref={bottomRef} />
      </pre>
    </div>
  )
}

// ─── Job Detail (expanded row) ───────────────────────────────────────────────
function JobDetail({ jobName, isRunning, onNavigate }) {
  const [limit, setLimit] = useState(20)
  const { data, loading } = useApi(`/api/jobs/${jobName}?runs=${limit}`, isRunning ? 3000 : 0)
  const runs              = data?.runs ?? []
  const [selId, setSelId] = useState(null)

  // Auto-select most recent run
  useEffect(() => {
    if (runs.length > 0 && !selId) setSelId(runs[0].id)
  }, [runs.length])

  const sel = runs.find(r => r.id === selId)

  // Fetch output variables for the selected run
  const { data: outputs } = useApi(selId ? `/api/runs/${selId}/outputs` : null)

  const job = data?.job

  return (
    <div class="py-3 px-4 bg-gray-50 space-y-3">
      {/* Job config summary */}
      {job && (
        <div class="flex flex-wrap gap-x-6 gap-y-1 text-xs text-gray-500">
          {job.timeout  && <span>timeout: <code class="text-gray-700">{job.timeout}</code></span>}
          {job.retries != null && <span>retries: <code class="text-gray-700">{job.retries}</code></span>}
          {job.concurrency && <span>concurrency: <code class="text-gray-700">{job.concurrency}</code></span>}
          {job.on_failure && <span>on_failure: <code class="text-gray-700">{job.on_failure}</code></span>}
          {job.sla && <span>sla: <code class="text-gray-700">{job.sla}</code></span>}
        </div>
      )}

      {loading && runs.length === 0 ? (
        <div class="flex items-center gap-2 text-xs text-gray-400"><Spinner sm />Loading runs…</div>
      ) : runs.length === 0 ? (
        <p class="text-xs text-gray-400">No runs yet.</p>
      ) : (
        <div>
          <p class="text-xs font-semibold uppercase tracking-wider text-gray-400 mb-2">Recent runs</p>
          <div class="flex flex-wrap gap-1.5">
            {runs.map(r => {
              const slaHit  = r.sla_breached
              const badgeSt = slaHit && r.status === 'RUNNING' ? 'SLA_BREACH' : r.status
              const dur = durSec(r.started_at, r.finished_at)
              return (
                <button
                  key={r.id}
                  onClick={() => onNavigate ? onNavigate(r.id) : null}
                  class="flex items-center gap-1.5 rounded px-2.5 py-1 text-xs border border-gray-200 bg-white hover:border-blue-400 hover:bg-blue-50 transition-colors"
                >
                  <Badge status={badgeSt} />
                  {slaHit && r.status !== 'RUNNING' && (
                    <span class="text-amber-600 font-semibold flex items-center gap-1" title="SLA breached"><AlertIcon className="w-3.5 h-3.5" /> SLA</span>
                  )}
                  <span class="text-gray-400">{fmtRel(r.started_at)}</span>
                  {dur != null && <span class="text-gray-400 font-mono">{dur}s</span>}
                  {r.reason && <span class="text-gray-400 italic truncate max-w-24" title={r.reason}>"{r.reason}"</span>}
                </button>
              )
            })}
          </div>
        </div>
      )}
      {runs.length > 0 && onNavigate && (
        <p class="text-xs text-gray-400">Click a run pill to open full Run Detail →</p>
      )}
      {runs.length >= limit && (
        <button
          onClick={() => setLimit(l => l + 20)}
          class="text-xs text-blue-600 hover:underline"
        >Show more…</button>
      )}
    </div>
  )
}

// ─── Jobs view ───────────────────────────────────────────────────────────────
function JobsView({ tags, showToast, onNavigate }) {
  const [tagFilter, setTagFilter] = useState('')
  const [expanded,  setExpanded]  = useState(null)

  const url              = tagFilter ? `/api/jobs?tag=${encodeURIComponent(tagFilter)}` : '/api/jobs'
  const { data: jobs, loading, reload } = useApi(url, 5000)

  function toggle(name) { setExpanded(prev => prev === name ? null : name) }

  async function runJob(e, name) {
    e.stopPropagation()
    const r = await apiPost(`/api/jobs/${name}/run`)
    showToast(r.ok ? `Triggered ${name}` : `Failed to trigger ${name}`, r.ok ? 'ok' : 'error')
    reload()
  }

  async function cancelJob(e, name) {
    e.stopPropagation()
    const r = await apiPost(`/api/jobs/${name}/cancel`)
    showToast(r.ok ? `Cancelled ${name}` : `Failed to cancel ${name}`, r.ok ? 'ok' : 'error')
    reload()
  }

  async function pauseJob(e, name) {
    e.stopPropagation()
    const r = await apiPost(`/api/jobs/${name}/pause`)
    showToast(r.ok ? `Paused ${name}` : `Failed to pause ${name}`, r.ok ? 'ok' : 'error')
    reload()
  }

  async function resumeJob(e, name) {
    e.stopPropagation()
    const r = await apiPost(`/api/jobs/${name}/resume`)
    showToast(r.ok ? `Resumed ${name}` : `Failed to resume ${name}`, r.ok ? 'ok' : 'error')
    reload()
  }

  return (
    <div class="space-y-0">
      {/* Tag health strip */}
      <TagHealthStrip jobs={jobs} tagFilter={tagFilter} setTagFilter={v => { setTagFilter(v); setExpanded(null) }} showToast={showToast} />

      <div class="p-5 space-y-4">
      {/* Toolbar */}
      <div class="flex items-center justify-between">
        <div class="flex items-center gap-2">
          <label class="text-sm font-medium text-gray-600">Tag:</label>
          <select
            value={tagFilter}
            onChange={e => { setTagFilter(e.target.value); setExpanded(null) }}
            class="rounded-md border border-gray-300 bg-white px-2.5 py-1.5 text-sm text-gray-700 shadow-sm focus:border-blue-500 focus:outline-none"
          >
            <option value="">All jobs</option>
            {(tags ?? []).map(t => <option key={t.tag} value={t.tag}>{t.tag} ({t.count})</option>)}
          </select>
        </div>
        <button
          onClick={reload}
          class="flex items-center gap-1.5 rounded-md border border-gray-300 bg-white px-3 py-1.5 text-sm font-medium text-gray-700 shadow-sm hover:bg-gray-50 transition-colors"
        >
          {loading ? <Spinner sm /> : <RefreshCwIcon className="w-4 h-4" />} Refresh
        </button>
      </div>

      {/* Table */}
      <div class="overflow-hidden rounded-xl border border-gray-200 shadow-sm bg-white">
        <table class="min-w-full divide-y divide-gray-200 text-sm">
          <thead class="bg-gray-50">
            <tr>
              <th class="w-8 px-3" />
              <Th>Job</Th>
              <Th>Tags</Th>
              <Th>Timezone</Th>
              <Th>Status</Th>
              <Th>Last run</Th>
              <Th>Next run</Th>
              <Th right>Actions</Th>
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-100">
            {!jobs || jobs.length === 0 ? (
              <EmptyRow cols={8} loading={loading} msg="No jobs found." />
            ) : (jobs ?? []).map(job => {
              const { ts, status } = jobLastRun(job)
              const isRunning      = job.running
              const isPaused       = job.paused
              const isOpen         = expanded === job.name
              return (
                <>
                  <tr
                    key={job.name}
                    onClick={() => toggle(job.name)}
                    class="cursor-pointer hover:bg-gray-50 transition-colors"
                  >
                    <td class="px-3 py-3 text-gray-400 text-xs select-none w-8">
                      <button
                        onClick={e => { e.stopPropagation(); toggle(job.name) }}
                        class="text-gray-400 hover:text-gray-600 p-1 flex items-center justify-center rounded-full hover:bg-gray-100"
                      >
                        {isOpen ? <ChevronDownSolidIcon className="w-4 h-4" /> : <ChevronRightSolidIcon className="w-4 h-4" />}
                      </button>
                    </td>
                    <td class="px-4 py-3">
                      <span class="font-mono font-medium text-gray-900">{job.name}</span>
                      {job.description && (
                        <p class="text-xs text-gray-400 mt-0.5 truncate max-w-xs">{job.description}</p>
                      )}
                    </td>
                    <td class="px-4 py-3">
                      <div class="flex flex-wrap gap-1">
                        {(job.tags ?? []).map(t => (
                          <span key={t} class="rounded-full bg-gray-100 px-2 py-0.5 text-xs text-gray-600">{t}</span>
                        ))}
                      </div>
                    </td>
                    <td class="px-4 py-3">
                      {job.timezone
                        ? <span class="rounded bg-gray-100 px-1.5 py-0.5 text-xs font-mono text-gray-600">{job.timezone}</span>
                        : <span class="text-xs text-gray-300">—</span>
                      }
                    </td>
                    <td class="px-4 py-3">
                      {isPaused
                        ? <Badge status="PAUSED" />
                        : status
                        ? <Badge status={status} />
                        : <span class="text-xs text-gray-400">never run</span>
                      }
                    </td>
                    <td class="px-4 py-3 text-xs text-gray-500">{fmtRel(ts)}</td>
                    <td class="px-4 py-3 text-xs text-gray-500 font-mono">
                      {isPaused
                        ? <span class="text-orange-500">paused</span>
                        : fmtNext(job.next_run)
                      }
                    </td>
                    <td class="px-4 py-3 text-right">
                      <div class="flex items-center justify-end gap-1" onClick={e => e.stopPropagation()}>
                        {isPaused ? (
                          <button
                            onClick={e => resumeJob(e, job.name)}
                            class="flex items-center gap-1.5 rounded px-2.5 py-1 text-xs font-medium bg-green-50 text-green-700 hover:bg-green-100 transition-colors border border-green-200"
                          >
                            <PlayIcon className="w-3.5 h-3.5" /> Resume
                          </button>
                        ) : isRunning ? (
                          <button
                            onClick={e => cancelJob(e, job.name)}
                            class="flex items-center gap-1.5 rounded px-2.5 py-1 text-xs font-medium bg-red-50 text-red-600 hover:bg-red-100 transition-colors border border-red-200"
                          >
                            <XIcon className="w-3.5 h-3.5" /> Cancel
                          </button>
                        ) : (
                          <>
                            <button
                              onClick={e => runJob(e, job.name)}
                              class="flex items-center gap-1.5 rounded px-2.5 py-1 text-xs font-medium bg-blue-50 text-blue-600 hover:bg-blue-100 transition-colors border border-blue-200"
                            >
                              <PlayIcon className="w-3.5 h-3.5" /> Run
                            </button>
                            <button
                              onClick={e => pauseJob(e, job.name)}
                              class="flex items-center gap-1.5 rounded px-2 py-1 text-xs font-medium bg-orange-50 text-orange-600 hover:bg-orange-100 transition-colors border border-orange-200"
                              title="Pause scheduling for this job"
                            >
                              <PauseIcon className="w-3.5 h-3.5" />
                            </button>
                          </>
                        )}
                      </div>
                    </td>
                  </tr>
                  {isOpen && (
                    <tr key={`${job.name}-detail`}>
                      <td colspan={8} class="p-0 border-t border-dashed border-gray-200">
                        <JobDetail jobName={job.name} isRunning={isRunning} onNavigate={onNavigate} />
                      </td>
                    </tr>
                  )}
                </>
              )
            })}
          </tbody>
        </table>
      </div>
      </div>
    </div>
  )
}

// ─── Audit view ───────────────────────────────────────────────────────────────
function AuditView({ tags, onNavigate }) {
  const { data: allJobs }     = useApi('/api/jobs', 0)
  const jobNames              = (allJobs ?? []).map(j => j.name)
  const [filters, setFilters] = useState({ job: '', status: '', trigger: '', since: '', tag: '' })

  function set(k, v) { setFilters(f => ({ ...f, [k]: v })) }

  const p = new URLSearchParams()
  Object.entries(filters).forEach(([k, v]) => {
    if (!v) return
    if (k === 'since') p.set(k, `${v}T00:00:00Z`)
    else               p.set(k, v)
  })

  const { data: runs, loading, reload } = useApi(`/api/audit${p.size ? '?' + p : ''}`, 15000)
  const list = runs ?? []

  return (
    <div class="p-5 space-y-4">
      <div class="flex flex-wrap gap-3 items-end rounded-xl border border-gray-200 bg-white p-4 shadow-sm">
        <FJobSel label="Job"     value={filters.job}     onChange={v => set('job', v)}  jobNames={jobNames} />
        <FSel    label="Status"  value={filters.status}  onChange={v => set('status', v)}
          opts={['','SUCCESS','FAILED','RUNNING','CANCELLED','SKIPPED','PENDING']} />
        <FSel    label="Trigger" value={filters.trigger} onChange={v => set('trigger', v)}
          opts={['','schedule','manual','dependency']} />
        <FSel    label="Tag"     value={filters.tag}     onChange={v => set('tag', v)}
          opts={['', ...(tags ?? []).map(t => t.tag)]} />
        <FInput  label="Since"   value={filters.since}   onChange={v => set('since', v)} type="date" />
        <button
          onClick={reload}
          class="ml-auto flex items-center gap-1.5 rounded-md border border-gray-300 bg-white px-3 py-1.5 text-sm font-medium text-gray-700 shadow-sm hover:bg-gray-50 transition-colors self-end"
        >
          {loading ? <Spinner sm /> : <RefreshCwIcon className="w-4 h-4" />} Refresh
        </button>
      </div>

      <div class="overflow-hidden rounded-xl border border-gray-200 shadow-sm bg-white">
        <table class="min-w-full divide-y divide-gray-200 text-sm">
          <thead class="bg-gray-50">
            <tr>
              {['ID','Job','Status','Attempt','Trigger','Reason','Started','Duration'].map(h => (
                <Th key={h}>{h}</Th>
              ))}
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-100">
            {list.length === 0 ? (
              <EmptyRow cols={8} loading={loading} msg="No runs match the current filters." />
            ) : list.map(r => {
              const d = durSec(r.started_at, r.finished_at)
              return (
                <tr
                  key={r.id}
                  onClick={() => onNavigate ? onNavigate(r.id) : null}
                  class="cursor-pointer transition-colors hover:bg-blue-50"
                >
                  <td class="px-4 py-3 font-mono text-xs text-gray-400">{r.id}</td>
                  <td class="px-4 py-3 font-mono font-medium text-gray-900">{r.job_name}</td>
                  <td class="px-4 py-3"><Badge status={r.status} /></td>
                  <td class="px-4 py-3 text-xs text-gray-500">{r.attempt}</td>
                  <td class="px-4 py-3 text-gray-500 capitalize">{r.trigger}</td>
                  <td class="px-4 py-3 text-gray-500 max-w-xs truncate italic text-xs" title={r.reason}>{r.reason || '—'}</td>
                  <td class="px-4 py-3 text-xs text-gray-500">{fmtRel(r.started_at)}</td>
                  <td class="px-4 py-3 text-xs font-mono text-gray-500">{d != null ? `${d}s` : '—'}</td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>

      {onNavigate && (
        <p class="text-xs text-gray-400 text-right">Click any row to open full Run Detail</p>
      )}
    </div>
  )
}

// ─── Outputs explorer ─────────────────────────────────────────────────────────
function OutputsView() {
  const { data: allJobs }           = useApi('/api/jobs', 0)
  const jobNames                    = (allJobs ?? []).map(j => j.name)
  const [filters, setFilters]       = useState({ job: '', cycle_id: '', run_id: '' })
  const [limit, setLimit]           = useState(100)

  function set(k, v) { setFilters(f => ({ ...f, [k]: v })) }

  const p = new URLSearchParams({ limit })
  if (filters.job)      p.set('job',      filters.job)
  if (filters.cycle_id) p.set('cycle_id', filters.cycle_id)
  if (filters.run_id)   p.set('run_id',   filters.run_id)

  const { data: rows, loading, reload } = useApi(`/api/db/run_outputs?${p}`, 0)
  const list = rows ?? []

  return (
    <div class="p-5 space-y-4">
      <div class="flex flex-wrap gap-3 items-end rounded-xl border border-gray-200 bg-white p-4 shadow-sm">
        <FJobSel label="Job" value={filters.job} onChange={v => set('job', v)} jobNames={jobNames} />
        <FInput label="Cycle ID" value={filters.cycle_id} onChange={v => set('cycle_id', v)} placeholder="uuid prefix…" />
        <FInput label="Run ID"   value={filters.run_id}   onChange={v => set('run_id', v)}   placeholder="numeric…" />
        <label class="flex flex-col gap-1">
          <span class="text-xs font-medium text-gray-500">Limit</span>
          <select
            value={limit}
            onChange={e => setLimit(Number(e.target.value))}
            class="rounded-md border border-gray-300 bg-white px-2.5 py-1.5 text-sm text-gray-700 shadow-sm focus:border-blue-500 focus:outline-none"
          >
            {[25, 50, 100, 250].map(n => <option key={n} value={n}>{n}</option>)}
          </select>
        </label>
        <button
          onClick={reload}
          class="ml-auto self-end flex items-center gap-1.5 rounded-md border border-gray-300 bg-white px-3 py-1.5 text-sm font-medium text-gray-700 shadow-sm hover:bg-gray-50 transition-colors"
        >
          {loading ? <Spinner sm /> : <RefreshCwIcon className="w-4 h-4" />} Refresh
        </button>
      </div>

      <div class="overflow-hidden rounded-xl border border-gray-200 shadow-sm bg-white">
        <table class="min-w-full divide-y divide-gray-200 text-sm">
          <thead class="bg-gray-50">
            <tr>
              {['Run ID','Job','Variable','Value','Cycle ID'].map(h => <Th key={h}>{h}</Th>)}
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-100">
            {list.length === 0 ? (
              <EmptyRow cols={5} loading={loading} msg="No output variables found." />
            ) : list.map((o, i) => (
              <tr key={i} class="hover:bg-gray-50">
                <td class="px-4 py-3 font-mono text-xs text-gray-400">{o.run_id}</td>
                <td class="px-4 py-3 font-mono text-xs text-gray-800">{o.job_name}</td>
                <td class="px-4 py-3 font-mono text-xs text-blue-700">{o.var_name}</td>
                <td class="px-4 py-3 font-mono text-xs text-gray-600 max-w-sm truncate" title={o.value}>{o.value}</td>
                <td class="px-4 py-3 font-mono text-xs text-gray-400">
                  <button
                    class="hover:text-blue-600 transition-colors"
                    title="Filter to this cycle"
                    onClick={() => set('cycle_id', o.cycle_id)}
                  >
                    {o.cycle_id}
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}

// ─── Data Explorer ────────────────────────────────────────────────────────────
const DATA_SUBTABS = ['job_runs', 'job_state', 'run_logs']

function DataView({ showToast, onNavigate }) {
  const [sub, setSub] = useState('job_runs')
  return (
    <div>
      <div class="flex gap-0 px-6 bg-gray-50 border-b border-gray-200">
        {DATA_SUBTABS.map(t => (
          <button
            key={t}
            onClick={() => setSub(t)}
            class={[
              'px-4 py-2 text-xs font-medium border-b-2 transition-colors',
              sub === t ? 'border-blue-500 text-blue-600' : 'border-transparent text-gray-500 hover:text-gray-700',
            ].join(' ')}
          >
            {t}
          </button>
        ))}
      </div>
      {sub === 'job_runs'  && <JobRunsTable  showToast={showToast} onNavigate={onNavigate} />}
      {sub === 'job_state' && <JobStateTable showToast={showToast} />}
      {sub === 'run_logs'  && <RunLogsSearch />}
    </div>
  )
}

function JobRunsTable({ showToast, onNavigate }) {
  const { data: allJobs }          = useApi('/api/jobs', 0)
  const jobNames                   = (allJobs ?? []).map(j => j.name)
  const [filters, setFilters]      = useState({ job: '', status: '', trigger: '', since: '', sla: false })
  const [pageSize, setPageSize]    = useState(50)
  const [page, setPage]            = useState(0)

  function set(k, v) { setFilters(f => ({ ...f, [k]: v })) }

  // Reset to first page when filters change
  useEffect(() => { setPage(0) }, [filters.job, filters.status, filters.trigger, filters.since, filters.sla, pageSize])

  const p = new URLSearchParams({ limit: pageSize, offset: page * pageSize })
  if (filters.job)     p.set('job',          filters.job)
  if (filters.status)  p.set('status',        filters.status)
  if (filters.trigger) p.set('trigger',       filters.trigger)
  if (filters.since)   p.set('since',         filters.since + 'T00:00:00Z')
  if (filters.sla)     p.set('sla_breached',  'true')

  const { data: runs, loading, reload } = useApi(`/api/db/job_runs?${p}`, 0)
  const list = runs ?? []

  return (
    <div class="p-5 space-y-4">
      <div class="rounded-xl border border-gray-200 bg-white p-4 shadow-sm space-y-3">
        <div class="flex flex-wrap gap-3 items-end">
          <FJobSel label="Job"     value={filters.job}     onChange={v => set('job', v)}     jobNames={jobNames} />
          <FSel    label="Status"  value={filters.status}  onChange={v => set('status', v)}
            opts={['','SUCCESS','FAILED','RUNNING','PENDING','CANCELLED','SKIPPED']} />
          <FSel    label="Trigger" value={filters.trigger} onChange={v => set('trigger', v)}
            opts={['','schedule','manual','dependency']} />
          <FInput  label="Since"   value={filters.since}   onChange={v => set('since', v)} type="date" />
          <div class="flex flex-col gap-1">
            <span class="text-xs font-medium text-gray-500">SLA</span>
            <button
              onClick={() => set('sla', !filters.sla)}
              class={`rounded-md border px-3 py-1.5 text-sm font-medium transition-colors ${
                filters.sla
                  ? 'border-amber-300 bg-amber-50 text-amber-700 hover:bg-amber-100'
                  : 'border-gray-300 bg-white text-gray-600 hover:bg-gray-50'
              }`}
            >
              {filters.sla ? '⚠ SLA breach only' : 'All runs'}
            </button>
          </div>
          <div class="ml-auto flex items-end gap-2">
            <div class="flex flex-col gap-1">
              <span class="text-xs font-medium text-gray-500">Page size</span>
              <select value={pageSize} onChange={e => setPageSize(Number(e.target.value))}
                class="rounded-md border border-gray-300 bg-white px-2.5 py-1.5 text-sm text-gray-700 shadow-sm focus:border-blue-500 focus:outline-none">
                {[25, 50, 100].map(n => <option key={n} value={n}>{n}</option>)}
              </select>
            </div>
            <button onClick={reload}
              class="flex items-center gap-1.5 rounded-md border border-gray-300 bg-white px-3 py-1.5 text-sm font-medium text-gray-700 shadow-sm hover:bg-gray-50 transition-colors">
              {loading ? <Spinner sm /> : <RefreshCwIcon className="w-4 h-4" />} Refresh
            </button>
          </div>
        </div>
      </div>

      <div class="overflow-hidden rounded-xl border border-gray-200 shadow-sm bg-white">
        <table class="min-w-full divide-y divide-gray-200 text-xs">
          <thead class="bg-gray-50">
            <tr>
              {['ID','Job','Status','Att','Trigger','By','Reason','Started','Finished','Exit','SLA','HC'].map(h => <Th key={h}>{h}</Th>)}
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-100">
            {list.length === 0 ? (
              <EmptyRow cols={12} loading={loading} msg="No runs match the current filters." />
            ) : list.map(r => (
              <tr key={r.id} class="hover:bg-gray-50">
                <td class="px-3 py-2 font-mono text-gray-400">
                  <button
                    class="hover:text-blue-600 hover:underline transition-colors"
                    title="Open Run Detail"
                    onClick={() => onNavigate && onNavigate(r.id)}
                  >
                    {r.id}
                  </button>
                </td>
                <td class="px-3 py-2 font-mono font-medium text-gray-900">{r.job_name}</td>
                <td class="px-3 py-2"><Badge status={r.status} /></td>
                <td class="px-3 py-2 text-gray-500 text-center">{r.attempt}</td>
                <td class="px-3 py-2 text-gray-500 capitalize">{r.trigger}</td>
                <td class="px-3 py-2 font-mono text-gray-400">{r.triggered_by}</td>
                <td class="px-3 py-2 italic text-gray-400 max-w-xs truncate" title={r.reason}>{r.reason || '—'}</td>
                <td class="px-3 py-2 font-mono text-gray-500 whitespace-nowrap">{r.started_at ? new Date(r.started_at).toLocaleString() : '—'}</td>
                <td class="px-3 py-2 font-mono text-gray-500 whitespace-nowrap">{r.finished_at ? new Date(r.finished_at).toLocaleString() : '—'}</td>
                <td class="px-3 py-2 font-mono text-gray-500 text-center">{r.exit_code ?? '—'}</td>
                <td class="px-3 py-2 text-center">
                  {r.sla_breached
                    ? <span class="text-xs font-bold text-amber-600 flex items-center gap-0.5 justify-center"><AlertIcon className="w-3 h-3" /> SLA</span>
                    : <span class="text-gray-300">—</span>}
                </td>
                <td class="px-3 py-2 text-gray-500 text-center">{r.hc_status ?? '—'}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <div class="flex items-center justify-end gap-2">
          <button
            disabled={page === 0}
            onClick={() => setPage(pg => Math.max(0, pg - 1))}
            class="rounded border border-gray-300 bg-white px-3 py-1.5 text-xs font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-40"
          >← Prev</button>
          <span class="text-xs text-gray-500">Page {page + 1}</span>
          <button
            disabled={list.length < pageSize}
            onClick={() => setPage(pg => pg + 1)}
            class="rounded border border-gray-300 bg-white px-3 py-1.5 text-xs font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-40"
          >Next →</button>
        </div>
    </div>
  )
}

function JobStateTable({ showToast }) {
  const { data: states, loading, reload } = useApi('/api/db/state', 0)
  const list = states ?? []

  async function clearLock(jobName) {
    const r = await apiPost(`/api/db/job_state/${encodeURIComponent(jobName)}/clear_lock`)
    const body = await r.json().catch(() => ({}))
    showToast(body.ok ? `Lock cleared for ${jobName}` : `Failed: ${body.error ?? 'unknown error'}`, body.ok ? 'ok' : 'error')
    reload()
  }

  return (
    <div class="p-5">
      <div class="overflow-hidden rounded-xl border border-gray-200 shadow-sm bg-white">
        <div class="flex items-center justify-between px-4 py-2.5 bg-gray-50 border-b border-gray-100">
          <span class="text-xs font-semibold text-gray-600">job_state — scheduler state per job</span>
          <button onClick={reload} class="flex items-center gap-1.5 text-xs text-gray-500 hover:text-gray-700 transition-colors">
            {loading ? <Spinner sm /> : <RefreshCwIcon className="w-3.5 h-3.5" />} Refresh
          </button>
        </div>
        <table class="min-w-full divide-y divide-gray-200 text-sm">
          <thead class="bg-gray-50">
            <tr>
              {['Job','Last Success','Last Failure','Next Run','Lock PID',''].map(h => <Th key={h}>{h}</Th>)}
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-100">
            {list.length === 0 ? (
              <EmptyRow cols={6} loading={loading} msg="No job state rows found." />
            ) : list.map(s => (
              <tr key={s.job_name} class={`hover:bg-gray-50 ${s.lock_pid != null ? 'bg-amber-50' : ''}`}>
                <td class="px-4 py-2.5 font-mono text-xs font-medium text-gray-900">{s.job_name}</td>
                <td class="px-4 py-2.5 text-xs text-gray-500">{fmtTs(s.last_success)}</td>
                <td class="px-4 py-2.5 text-xs text-gray-500">{fmtTs(s.last_failure)}</td>
                <td class="px-4 py-2.5 text-xs font-mono text-gray-500">{fmtTs(s.next_run)}</td>
                <td class="px-4 py-2.5 text-xs">
                  {s.lock_pid != null
                    ? <span class="flex items-center gap-1 text-amber-700 font-mono font-semibold">
                        <AlertIcon className="w-3.5 h-3.5" /> {s.lock_pid}
                      </span>
                    : <span class="text-gray-300">—</span>}
                </td>
                <td class="px-4 py-2.5 text-right">
                  {s.lock_pid != null && (
                    <button
                      onClick={() => clearLock(s.job_name)}
                      class="text-xs font-medium text-red-600 hover:text-red-800 border border-red-200 rounded px-2.5 py-1 hover:bg-red-50 transition-colors"
                    >
                      Clear lock
                    </button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}

function RunLogsSearch() {
  const { data: allJobs }           = useApi('/api/jobs', 0)
  const jobNames                    = (allJobs ?? []).map(j => j.name)
  const [filters, setFilters]       = useState({ run_id: '', job: '', stream: '', q: '' })
  const [limit, setLimit]           = useState(200)
  const [committed, setCommitted]   = useState({ run_id: '', job: '', stream: '', q: '', limit: 200 })

  function set(k, v) { setFilters(f => ({ ...f, [k]: v })) }

  // Dropdowns and stream select commit immediately
  function setAndCommit(k, v) {
    const next = { ...filters, [k]: v }
    setFilters(next)
    setCommitted({ ...next, limit })
  }

  function search() { setCommitted({ ...filters, limit }) }

  const p = new URLSearchParams({ limit: committed.limit })
  if (committed.run_id) p.set('run_id', committed.run_id)
  if (committed.job)    p.set('job',    committed.job)
  if (committed.stream) p.set('stream', committed.stream)
  if (committed.q)      p.set('q',      committed.q)

  const { data: lines, loading } = useApi(`/api/db/run_logs?${p}`, 0)
  const list = lines ?? []

  function rowCls(stream) {
    if (stream === 'stderr')      return 'bg-red-50'
    if (stream === 'healthcheck') return 'bg-amber-50'
    return ''
  }

  return (
    <div class="p-5 space-y-4">
      <div class="flex flex-wrap gap-3 items-end rounded-xl border border-gray-200 bg-white p-4 shadow-sm">
        <FInput   label="Run ID"  value={filters.run_id} onChange={v => set('run_id', v)} placeholder="numeric…" />
        <FJobSel  label="Job"     value={filters.job}    onChange={v => setAndCommit('job', v)} jobNames={jobNames} />
        <FSel     label="Stream"  value={filters.stream} onChange={v => setAndCommit('stream', v)}
          opts={['', 'stdout', 'stderr', 'healthcheck']} />
        <FInput   label="Keyword" value={filters.q}      onChange={v => set('q', v)}      placeholder="search text…" />
        <label class="flex flex-col gap-1">
          <span class="text-xs font-medium text-gray-500">Limit</span>
          <select value={limit} onChange={e => setLimit(Number(e.target.value))}
            class="rounded-md border border-gray-300 bg-white px-2.5 py-1.5 text-sm text-gray-700 shadow-sm focus:border-blue-500 focus:outline-none">
            {[100, 200, 500].map(n => <option key={n} value={n}>{n}</option>)}
          </select>
        </label>
        <button onClick={search}
          class="ml-auto self-end flex items-center gap-1.5 rounded-md border border-blue-300 bg-blue-50 px-3 py-1.5 text-sm font-medium text-blue-700 shadow-sm hover:bg-blue-100 transition-colors">
          {loading ? <Spinner sm /> : null} Search
        </button>
      </div>

      <div class="overflow-hidden rounded-xl border border-gray-200 shadow-sm bg-white">
        <table class="min-w-full divide-y divide-gray-200">
          <thead class="bg-gray-50">
            <tr>
              {['Run','Seq','Stream','Timestamp','Line'].map(h => <Th key={h}>{h}</Th>)}
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-100 font-mono text-xs">
            {list.length === 0 ? (
              <EmptyRow cols={5} loading={loading} msg="No log lines found." />
            ) : list.map((l, i) => (
              <tr key={i} class={`${rowCls(l.stream)} hover:opacity-80`}>
                <td class="px-3 py-1.5 text-gray-400">{l.run_id}</td>
                <td class="px-3 py-1.5 text-gray-300">{l.seq}</td>
                <td class="px-3 py-1.5">
                  <span class={l.stream === 'stderr' ? 'text-red-600 font-medium' : l.stream === 'healthcheck' ? 'text-amber-600 font-medium' : 'text-gray-500'}>
                    {l.stream}
                  </span>
                </td>
                <td class="px-3 py-1.5 text-gray-400 whitespace-nowrap">{l.ts?.slice(0, 19)?.replace('T', ' ')}</td>
                <td class="px-3 py-1.5 text-gray-700 max-w-2xl truncate" title={l.line}>{l.line}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}

// ─── Alerts explorer ──────────────────────────────────────────────────────────
const ALERT_STATUS_CLS = {
  delivered: 'bg-green-100 text-green-700 ring-green-600/20',
  failed:    'bg-red-100   text-red-700   ring-red-600/20',
  pending:   'bg-yellow-100 text-yellow-700 ring-yellow-600/20',
}

function AlertsView({ showToast }) {
  const { data: allJobs }          = useApi('/api/jobs', 0)
  const jobNames                   = (allJobs ?? []).map(j => j.name)
  const [jobFilter,    setJobFilter]    = useState('')
  const [statusFilter, setStatusFilter] = useState('')
  const [limit, setLimit]               = useState(25)
  const [offset, setOffset]             = useState(0)
  const [expId, setExpId]               = useState(null)
  const [loadingRetry, setLoadingRetry] = useState(null)

  // Reset offset when filters change
  useEffect(() => { setOffset(0) }, [jobFilter, statusFilter, limit])

  const p = new URLSearchParams({ limit, offset })
  if (jobFilter)    p.set('job',    jobFilter)
  if (statusFilter) p.set('status', statusFilter)

  const { data: rows, loading, reload } = useApi(`/api/db/alerts?${p}`, 30000)
  const list = rows ?? []

  async function retry(e, id) {
    e.stopPropagation()
    setLoadingRetry(id)
    const r = await apiPost(`/api/db/alerts/${id}/retry`)
    const body = await r.json().catch(() => ({}))
    setLoadingRetry(null)
    showToast(body.ok ? 'Alert queued for retry' : `Retry failed: ${body.error ?? 'unknown error'}`, body.ok ? 'ok' : 'error')
    reload()
  }

  return (
    <div class="p-5 space-y-4">
      <div class="flex flex-wrap gap-3 items-end rounded-xl border border-gray-200 bg-white p-4 shadow-sm">
        <FJobSel label="Job" value={jobFilter} onChange={setJobFilter} jobNames={jobNames} />
        <FSel   label="Status" value={statusFilter} onChange={setStatusFilter}
          opts={['', 'delivered', 'failed', 'pending']} />
        <label class="flex flex-col gap-1">
          <span class="text-xs font-medium text-gray-500">Per page</span>
          <select value={limit} onChange={e => setLimit(Number(e.target.value))}
            class="rounded-md border border-gray-300 bg-white px-2.5 py-1.5 text-sm text-gray-700 shadow-sm">
            {[25, 50, 100].map(n => <option key={n} value={n}>{n}</option>)}
          </select>
        </label>
        <button onClick={reload}
          class="ml-auto self-end flex items-center gap-1.5 rounded-md border border-gray-300 bg-white px-3 py-1.5 text-sm font-medium text-gray-700 shadow-sm hover:bg-gray-50 transition-colors">
          {loading ? <Spinner sm /> : <RefreshCwIcon className="w-4 h-4" />} Refresh
        </button>
      </div>

      <div class="overflow-hidden rounded-xl border border-gray-200 shadow-sm bg-white">
        <table class="min-w-full divide-y divide-gray-200 text-sm">
          <thead class="bg-gray-50">
            <tr>
              {['ID','Job','Run','Event','Channel','Status','Att','Last Attempt','Error','Payload',''].map(h => <Th key={h}>{h}</Th>)}
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-100">
            {list.length === 0 ? (
              <EmptyRow cols={11} loading={loading} msg="No alerts recorded yet." />
            ) : list.map(a => (
              <>
                <tr
                  key={a.id}
                  class="cursor-pointer hover:bg-gray-50 transition-colors"
                  onClick={() => setExpId(expId === a.id ? null : a.id)}
                >
                  <td class="px-4 py-3 font-mono text-xs text-gray-400">{a.id}</td>
                  <td class="px-4 py-3 font-mono text-xs text-gray-800">{a.job_name}</td>
                  <td class="px-4 py-3 font-mono text-xs text-gray-500">{a.run_id ?? '—'}</td>
                  <td class="px-4 py-3 text-xs text-gray-500">{a.event || '—'}</td>
                  <td class="px-4 py-3 text-xs text-gray-600">{a.channel}</td>
                  <td class="px-4 py-3">
                    <span class={`inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium ring-1 ring-inset ${ALERT_STATUS_CLS[a.status] ?? 'bg-gray-100 text-gray-600 ring-gray-500/20'}`}>
                      {a.status || 'delivered'}
                    </span>
                  </td>
                  <td class="px-4 py-3 text-xs text-gray-500 text-center">{a.attempts ?? 1}</td>
                  <td class="px-4 py-3 text-xs text-gray-500">{a.last_attempt_at ? fmtTs(a.last_attempt_at) : fmtTs(a.sent_at)}</td>
                  <td class="px-4 py-3 text-xs text-red-600 max-w-xs truncate font-mono" title={a.error}>{a.error || '—'}</td>
                  <td class="px-4 py-3 text-xs text-gray-400 max-w-xs truncate font-mono">
                    {a.payload ? a.payload.slice(0, 60) + (a.payload.length > 60 ? '…' : '') : '—'}
                  </td>
                  <td class="px-4 py-3 text-right" onClick={e => e.stopPropagation()}>
                    {a.status === 'failed' && (
                      <button
                        onClick={e => retry(e, a.id)}
                        disabled={loadingRetry === a.id}
                        class="flex items-center gap-1.5 text-xs font-medium text-blue-600 hover:text-blue-800 border border-blue-200 rounded px-2.5 py-1 hover:bg-blue-50 transition-colors disabled:opacity-50"
                      >
                        {loadingRetry === a.id ? <Spinner sm /> : <ReloadIcon className="w-3.5 h-3.5" />} Retry
                      </button>
                    )}
                  </td>
                </tr>
                {expId === a.id && a.payload && (
                  <tr key={`${a.id}-payload`}>
                    <td colspan={11} class="px-4 pb-3 bg-gray-50">
                      <pre class="text-xs font-mono text-gray-700 whitespace-pre-wrap bg-white rounded border border-gray-200 p-3 overflow-x-auto max-h-64">
                        {(() => { try { return JSON.stringify(JSON.parse(a.payload), null, 2) } catch { return a.payload } })()}
                      </pre>
                    </td>
                  </tr>
                )}
              </>
            ))}
          </tbody>
        </table>
      </div>

      {/* Pagination */}
      <div class="flex items-center justify-between">
        <span class="text-xs text-gray-400">
          Showing {offset + 1}–{offset + list.length}
        </span>
        <div class="flex items-center gap-2">
          <button
            disabled={offset === 0}
            onClick={() => setOffset(o => Math.max(0, o - limit))}
            class="rounded border border-gray-300 bg-white px-3 py-1.5 text-xs font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-40"
          >← Prev</button>
          <span class="text-xs text-gray-500">Page {Math.floor(offset / limit) + 1}</span>
          <button
            disabled={list.length < limit}
            onClick={() => setOffset(o => o + limit)}
            class="rounded border border-gray-300 bg-white px-3 py-1.5 text-xs font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-40"
          >Next →</button>
        </div>
      </div>
    </div>
  )
}

// ─── Run Detail full page ─────────────────────────────────────────────────────
function RunDetailView({ runId, onBack, onNavigate, showToast }) {
  const { data: run, loading: runLoading } = useApi(`/api/runs/${runId}`, 5000)
  const { data: outputs }                  = useApi(`/api/runs/${runId}/outputs`)
  const { data: jobData }                  = useApi(run?.job_name ? `/api/jobs/${run.job_name}` : null)
  const adjRuns                            = jobData?.runs ?? []
  const job                                = jobData?.job

  // SLA progress calculation
  const slaSec    = job?.sla  ? goDurSec(job.sla)  : null
  const durS      = run       ? durSec(run.started_at, run.finished_at ?? new Date().toISOString()) : null
  const slaPct    = slaSec && durS != null ? Math.min(100, Math.round((durS / slaSec) * 100)) : null
  const slaColor  = slaPct == null ? 'bg-gray-200' : slaPct < 70 ? 'bg-green-500' : slaPct < 90 ? 'bg-amber-500' : 'bg-red-500'

  // Prev/next navigation in the adjacent runs list
  const runIdx  = adjRuns.findIndex(r => r.id === runId)
  const prevRun = runIdx > 0            ? adjRuns[runIdx - 1] : null
  const nextRun = runIdx < adjRuns.length - 1 ? adjRuns[runIdx + 1] : null

  async function retryRun() {
    const r = await apiPost(`/api/jobs/${run.job_name}/retry`)
    showToast(r.ok ? 'Job queued for retry' : 'Retry failed', r.ok ? 'ok' : 'error')
  }
  async function skipRun() {
    const r = await apiPost(`/api/jobs/${run.job_name}/skip`)
    showToast(r.ok ? 'Next run skipped' : 'Skip failed', r.ok ? 'ok' : 'error')
  }
  async function cancelRun() {
    const r = await apiPost(`/api/jobs/${run.job_name}/cancel`)
    showToast(r.ok ? 'Run cancelled' : 'Cancel failed', r.ok ? 'ok' : 'error')
  }

  async function copyText(text) {
    try { await navigator.clipboard.writeText(text); showToast('Copied!', 'ok') }
    catch { showToast('Copy failed', 'error') }
  }

  if (runLoading && !run) return (
    <div class="flex items-center justify-center h-40 gap-2 text-gray-400"><Spinner />Loading run {runId}…</div>
  )
  if (!run) return (
    <div class="p-8 text-center text-gray-500">Run {runId} not found.</div>
  )

  return (
    <div class="p-5 max-w-5xl mx-auto space-y-5">
      {/* Breadcrumb & navigation */}
      <div class="flex items-center gap-2 text-sm">
        <button onClick={onBack} class="text-blue-600 hover:underline">← Back</button>
        <span class="text-gray-400">/</span>
        <span class="font-mono text-gray-600">{run.job_name}</span>
        <span class="text-gray-400">/</span>
        <span class="font-mono text-gray-800">Run #{run.id}</span>
        <div class="ml-auto flex items-center gap-1">
          {prevRun && (
            <button onClick={() => onNavigate(prevRun.id)} class="rounded border border-gray-300 bg-white px-2.5 py-1 text-xs font-medium text-gray-600 hover:bg-gray-50">← #{prevRun.id}</button>
          )}
          {nextRun && (
            <button onClick={() => onNavigate(nextRun.id)} class="rounded border border-gray-300 bg-white px-2.5 py-1 text-xs font-medium text-gray-600 hover:bg-gray-50">#{nextRun.id} →</button>
          )}
        </div>
      </div>

      {/* Run header card */}
      <div class="rounded-xl border border-gray-200 bg-white p-5 shadow-sm space-y-4">
        <div class="flex flex-wrap items-center gap-3">
          <Badge status={run.sla_breached && run.status === 'RUNNING' ? 'SLA_BREACH' : run.status} />
          <span class="font-mono font-semibold text-lg text-gray-800">{run.job_name}</span>
          <span class="text-gray-400 text-sm">Attempt {run.attempt}</span>
          {run.sla_breached && <span class="text-amber-600 text-sm font-semibold flex items-center gap-1"><AlertIcon className="w-4 h-4" /> SLA breached</span>}
          {/* Actions */}
          <div class="ml-auto flex items-center gap-2">
            {run.status === 'FAILED'  && <button onClick={retryRun}  class="flex items-center gap-1.5 rounded border border-blue-200 bg-blue-50 px-3 py-1.5 text-xs font-medium text-blue-700 hover:bg-blue-100"><ReloadIcon className="w-3.5 h-3.5" /> Retry</button>}
            {run.status === 'PENDING' && <button onClick={skipRun}   class="flex items-center gap-1.5 rounded border border-gray-200 bg-gray-50 px-3 py-1.5 text-xs font-medium text-gray-600 hover:bg-gray-100"><StopIcon className="w-3.5 h-3.5" /> Skip</button>}
            {run.status === 'RUNNING' && <button onClick={cancelRun} class="flex items-center gap-1.5 rounded border border-red-200 bg-red-50 px-3 py-1.5 text-xs font-medium text-red-600 hover:bg-red-100"><XIcon className="w-3.5 h-3.5" /> Cancel</button>}
          </div>
        </div>

        {/* Metadata grid */}
        <div class="grid grid-cols-2 gap-x-8 gap-y-1.5 text-xs">
          <div class="flex justify-between"><span class="text-gray-500">Run ID</span>
            <span class="font-mono text-gray-800 flex items-center gap-1">{run.id}
              <button onClick={() => copyText(String(run.id))} class="text-gray-400 hover:text-blue-600 ml-1" title="Copy"><EditIcon className="w-3 h-3" /></button>
            </span>
          </div>
          <div class="flex justify-between"><span class="text-gray-500">Trigger</span><span class="text-gray-800 capitalize">{run.trigger || '—'}</span></div>
          <div class="flex justify-between"><span class="text-gray-500">Triggered by</span><span class="font-mono text-gray-800">{run.triggered_by || '—'}</span></div>
          <div class="flex justify-between"><span class="text-gray-500">Exit code</span><span class="font-mono text-gray-800">{run.exit_code ?? '—'}</span></div>
          <div class="flex justify-between"><span class="text-gray-500">Started</span><span class="font-mono text-gray-800">{run.started_at ? fmtTs(run.started_at) : '—'}</span></div>
          <div class="flex justify-between"><span class="text-gray-500">Finished</span><span class="font-mono text-gray-800">{run.finished_at ? fmtTs(run.finished_at) : '—'}</span></div>
          <div class="flex justify-between"><span class="text-gray-500">Duration</span><span class="font-mono text-gray-800">{durS != null ? `${durS}s` : '—'}</span></div>
          <div class="flex justify-between"><span class="text-gray-500">HC status</span><span class="font-mono text-gray-800">{run.hc_status || '—'}</span></div>
          {run.reason && (
            <div class="col-span-2 flex justify-between"><span class="text-gray-500">Reason</span><span class="italic text-gray-700">{run.reason}</span></div>
          )}
        </div>

        {/* SLA progress */}
        {slaSec && (
          <div class="space-y-1">
            <div class="flex items-center justify-between text-xs">
              <span class="text-gray-500">SLA: {job.sla}</span>
              <span class={`font-medium ${slaPct >= 90 ? 'text-red-600' : slaPct >= 70 ? 'text-amber-600' : 'text-green-600'}`}>{slaPct}%</span>
            </div>
            <div class="h-2 w-full rounded-full bg-gray-100">
              <div class={`h-2 rounded-full transition-all ${slaColor}`} style={`width:${slaPct}%`} />
            </div>
          </div>
        )}
      </div>

      {/* Log output */}
      <div class="rounded-xl border border-gray-200 bg-white overflow-hidden shadow-sm">
        <div class="flex items-center justify-between px-4 py-2.5 bg-gray-50 border-b border-gray-100">
          <span class="text-xs font-semibold text-gray-600">Log output</span>
          {run.status === 'RUNNING' && <span class="flex items-center gap-1.5 text-xs text-blue-600"><span class="h-1.5 w-1.5 rounded-full bg-blue-400 animate-pulse"/> Live</span>}
        </div>
        <LogViewer runId={run.id} live={run.status === 'RUNNING'} />
      </div>

      {/* Output variables */}
      {outputs && outputs.length > 0 && (
        <div class="rounded-xl border border-gray-200 bg-white overflow-hidden shadow-sm">
          <div class="px-4 py-2.5 bg-gray-50 border-b border-gray-100 flex items-center">
            <span class="text-xs font-semibold text-gray-600">Output variables ({outputs.length})</span>
            {outputs[0]?.cycle_id && (
              <button
                class="ml-auto flex items-center gap-1.5 text-xs text-blue-600 hover:text-blue-800 hover:underline"
                onClick={() => { window.location.hash = `#/dag?cycle=${encodeURIComponent(outputs[0].cycle_id)}` }}
              >
                <ActivityIcon className="w-3.5 h-3.5" /> Show in DAG
              </button>
            )}
          </div>
          <table class="min-w-full divide-y divide-gray-100 text-xs">
            <thead class="bg-gray-50">
              <tr>
                <Th>Variable</Th><Th>Value</Th><Th>Cycle</Th><Th right></Th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-50">
              {outputs.map(o => (
                <tr key={o.var_name} class="hover:bg-gray-50">
                  <td class="px-3 py-2 font-mono text-gray-700">{o.var_name}</td>
                  <td class="px-3 py-2 font-mono text-gray-600 max-w-sm truncate" title={o.value}>{o.value}</td>
                  <td class="px-3 py-2 font-mono text-gray-400">{o.cycle_id?.slice(0,8)}</td>
                  <td class="px-3 py-2">
                    <button onClick={() => copyText(o.value)} class="text-gray-400 hover:text-blue-600 p-0.5" title="Copy value"><EditIcon className="w-3 h-3" /></button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}

// ─── DAG view ─────────────────────────────────────────────────────────────────
function DagView({ onNavigate, showToast, cycleId, onCycleDismiss }) {
  const { data: dagData, loading } = useApi('/api/dag')
  const { data: jobs }             = useApi('/api/jobs', 10000)
  const [ctxMenu, setCtxMenu]      = useState(null) // { name, x, y } | null
  const nodes    = dagData ?? []
  const hasEdges = nodes.some(n => n.dependencies?.length > 0)

  // Cycle overlay: which jobs participated in this cycle_id?
  const { data: cycleOutputs } = useApi(
    cycleId ? `/api/db/run_outputs?cycle_id=${encodeURIComponent(cycleId)}&limit=200` : null, 0)
  const cycleJobs = useMemo(() => {
    const s = new Set()
    for (const o of cycleOutputs ?? []) s.add(o.job_name)
    return s
  }, [cycleOutputs])

  // Close context menu when the user clicks elsewhere
  useEffect(() => {
    if (!ctxMenu) return
    const close = () => setCtxMenu(null)
    document.addEventListener('click', close)
    return () => document.removeEventListener('click', close)
  }, [!!ctxMenu])

  // Build a status map for colouring nodes
  const statusMap = useMemo(() => {
    const m = {}
    for (const j of jobs ?? []) m[j.name] = j
    return m
  }, [jobs])

  // Navigate to most-recent run for a job (fetches from job_runs API)
  async function goToLastRun(jobName) {
    try {
      const res  = await fetch(`/api/db/job_runs?job=${encodeURIComponent(jobName)}&limit=1`)
      const data = await res.json()
      if (Array.isArray(data) && data.length > 0 && data[0].id) {
        onNavigate && onNavigate(data[0].id)
      } else {
        showToast && showToast('No runs recorded for ' + jobName, 'error')
      }
    } catch {
      showToast && showToast('Failed to load run', 'error')
    }
  }

  // Context-menu actions
  async function ctxTrigger() {
    const name = ctxMenu.name; setCtxMenu(null)
    const r = await fetch(`/api/jobs/${name}/run`, { method: 'POST' })
    showToast && showToast(r.ok ? `Triggered ${name}` : 'Trigger failed', r.ok ? 'ok' : 'error')
  }
  async function ctxCancel() {
    const name = ctxMenu.name; setCtxMenu(null)
    const r = await fetch(`/api/jobs/${name}/cancel`, { method: 'POST' })
    showToast && showToast(r.ok ? `Cancelling ${name}` : 'Cancel failed', r.ok ? 'ok' : 'error')
  }
  async function ctxViewLogs() {
    const name = ctxMenu.name; setCtxMenu(null)
    await goToLastRun(name)
  }

  // Node colour based on status + cycle participation
  function nodeColors(name) {
    const st = statusMap[name]
    if (cycleId && cycleJobs.has(name))  return { fill: '#fef3c7', stroke: '#f59e0b', text: '#92400e' }
    if (st?.running)                     return { fill: '#dbeafe', stroke: '#93c5fd', text: '#1d4ed8' }
    if (st?.paused)                      return { fill: '#fff7ed', stroke: '#fed7aa', text: '#c2410c' }
    return { fill: '#f9fafb', stroke: '#e5e7eb', text: '#374151' }
  }

  // Kahn's topological rank for SVG layout
  const ranked = useMemo(() => {
    if (!hasEdges) return []
    const inDeg = {}, adj = {}
    for (const n of nodes) { inDeg[n.name] = 0; adj[n.name] = [] }
    for (const n of nodes) {
      for (const dep of n.dependencies ?? []) {
        adj[dep] = adj[dep] ?? []
        adj[dep].push(n.name)
        inDeg[n.name] = (inDeg[n.name] ?? 0) + 1
      }
    }
    const rank = {}
    const queue = Object.keys(inDeg).filter(k => inDeg[k] === 0)
    while (queue.length) {
      const cur = queue.shift()
      rank[cur] = rank[cur] ?? 0
      for (const next of adj[cur] ?? []) {
        inDeg[next]--
        rank[next] = Math.max(rank[next] ?? 0, rank[cur] + 1)
        if (inDeg[next] === 0) queue.push(next)
      }
    }
    const cols = {}
    for (const [name, r] of Object.entries(rank)) {
      if (!cols[r]) cols[r] = []
      cols[r].push(name)
    }
    return cols
  }, [nodes, hasEdges])

  // Shared cycle banner shown above the graph
  const cycleBanner = cycleId ? (
    <div class="flex items-center gap-2 rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-800">
      <span class="font-semibold">Cycle trace:</span>
      <span class="font-mono truncate max-w-xs">{cycleId}</span>
      <span class="text-amber-600 shrink-0">— {cycleJobs.size} job{cycleJobs.size !== 1 ? 's' : ''} participated</span>
      {onCycleDismiss && (
        <button onClick={onCycleDismiss} class="ml-auto flex items-center gap-1 text-amber-700 hover:text-amber-900">
          <XIcon className="w-3 h-3" /> Clear
        </button>
      )}
    </div>
  ) : null

  // Right-click context menu (fixed-position portal)
  const ctxMenuEl = ctxMenu ? (
    <div
      style={`position:fixed;left:${ctxMenu.x}px;top:${ctxMenu.y}px`}
      class="z-50 min-w-40 rounded-lg border border-gray-200 bg-white shadow-xl py-1 text-sm"
      onClick={e => e.stopPropagation()}
    >
      <div class="px-3 py-1.5 text-xs font-semibold text-gray-400 border-b border-gray-100">{ctxMenu.name}</div>
      <button onClick={ctxTrigger}  class="w-full text-left px-3 py-2 hover:bg-gray-50 flex items-center gap-2 text-gray-700"><PlayIcon className="w-3.5 h-3.5 text-green-600" /> Trigger</button>
      <button onClick={ctxCancel}   class="w-full text-left px-3 py-2 hover:bg-gray-50 flex items-center gap-2 text-gray-700"><XIcon className="w-3.5 h-3.5 text-red-500" /> Cancel</button>
      <div class="my-0.5 border-t border-gray-100" />
      <button onClick={ctxViewLogs} class="w-full text-left px-3 py-2 hover:bg-gray-50 flex items-center gap-2 text-gray-700"><ActivityIcon className="w-3.5 h-3.5 text-blue-500" /> View logs</button>
    </div>
  ) : null

  if (!hasEdges) {
    return (
      <div class="p-5 space-y-2">
        {cycleBanner}
        <div class="rounded-xl border border-gray-200 bg-white p-5 shadow-sm">
          <h2 class="text-sm font-semibold text-gray-700 mb-4">Job Dependency Graph</h2>
          {loading ? (
            <div class="flex items-center gap-2 text-sm text-gray-400"><Spinner />Loading…</div>
          ) : (
            <>
              <p class="text-sm text-gray-400 mb-4">No dependencies configured — all jobs run independently.</p>
              <ol class="flex flex-wrap gap-2 items-center">
                {nodes.map((n, i) => {
                  const { fill, stroke, text } = nodeColors(n.name)
                  const st = statusMap[n.name]
                  return (
                    <>
                      {i > 0 && <span key={`s${i}`} class="text-gray-300">→</span>}
                      <li key={n.name}
                        class="font-mono text-sm rounded px-2.5 py-1 cursor-pointer border transition-colors"
                        style={`background:${fill};border-color:${stroke};color:${text}`}
                        onClick={() => goToLastRun(n.name)}
                        onContextMenu={e => { e.preventDefault(); setCtxMenu({ name: n.name, x: e.clientX, y: e.clientY }) }}
                        title={st?.running ? 'Running — click for last run' : st?.paused ? 'Paused' : 'Click for last run · right-click for actions'}
                      >{n.name}</li>
                    </>
                  )
                })}
              </ol>
            </>
          )}
        </div>
        {ctxMenuEl}
      </div>
    )
  }

  // SVG layout constants
  const COL_W = 200, ROW_H = 72, PAD_X = 40, PAD_Y = 40
  const NODE_W = 160, NODE_H = 40

  // Assign (col, row) positions
  const pos = {}
  const colCount = {}
  for (const [col, names] of Object.entries(ranked)) {
    for (let row = 0; row < names.length; row++) {
      pos[names[row]] = { x: PAD_X + Number(col) * COL_W, y: PAD_Y + row * ROW_H }
      colCount[col] = (colCount[col] ?? 0) + 1
    }
  }
  const maxCol = Math.max(...Object.keys(ranked).map(Number))
  const maxRow = Math.max(...Object.values(colCount))
  const svgW = PAD_X * 2 + (maxCol + 1) * COL_W
  const svgH = PAD_Y * 2 + maxRow * ROW_H

  // Build edges — cycle edges are rendered in amber
  const edges = []
  for (const n of nodes) {
    for (const dep of n.dependencies ?? []) {
      if (pos[dep] && pos[n.name]) {
        const x1 = pos[dep].x + NODE_W, y1 = pos[dep].y + NODE_H / 2
        const x2 = pos[n.name].x,       y2 = pos[n.name].y + NODE_H / 2
        const inCycle = cycleId && cycleJobs.has(dep) && cycleJobs.has(n.name)
        edges.push({ x1, y1, x2, y2, key: `${dep}->${n.name}`,
          stroke: inCycle ? '#f59e0b' : '#d1d5db',
          width:  inCycle ? 2.5 : 2,
          marker: inCycle ? 'url(#arrow-cycle)' : 'url(#arrow)',
        })
      }
    }
  }

  return (
    <div class="p-5 space-y-2">
      {cycleBanner}
      <div class="rounded-xl border border-gray-200 bg-white p-5 shadow-sm overflow-x-auto">
        <h2 class="text-sm font-semibold text-gray-700 mb-4">Job Dependency Graph</h2>
        <svg width={svgW} height={svgH} style="font-family: monospace">
          <defs>
            <marker id="arrow" markerWidth="8" markerHeight="8" refX="6" refY="3" orient="auto">
              <path d="M0,0 L0,6 L8,3 z" fill="#9ca3af" />
            </marker>
            <marker id="arrow-cycle" markerWidth="8" markerHeight="8" refX="6" refY="3" orient="auto">
              <path d="M0,0 L0,6 L8,3 z" fill="#f59e0b" />
            </marker>
          </defs>
          {edges.map(e => (
            <path
              key={e.key}
              d={`M${e.x1},${e.y1} C${e.x1 + 40},${e.y1} ${e.x2 - 40},${e.y2} ${e.x2},${e.y2}`}
              fill="none" stroke={e.stroke} stroke-width={e.width} marker-end={e.marker}
            />
          ))}
          {nodes.map(n => {
            if (!pos[n.name]) return null
            const { x, y } = pos[n.name]
            const { fill, stroke, text } = nodeColors(n.name)
            const st = statusMap[n.name]
            const strokeW = cycleId && cycleJobs.has(n.name) ? 2.5 : 1.5
            return (
              <g
                key={n.name}
                style="cursor:pointer"
                onClick={() => goToLastRun(n.name)}
                onContextMenu={e => { e.preventDefault(); setCtxMenu({ name: n.name, x: e.clientX, y: e.clientY }) }}
              >
                <rect x={x} y={y} width={NODE_W} height={NODE_H} rx="8"
                  fill={fill} stroke={stroke} stroke-width={strokeW} />
                <text x={x + NODE_W / 2} y={y + NODE_H / 2 + 4} text-anchor="middle"
                  font-size="12" fill={text} font-weight="500">
                  {n.name.length > 20 ? n.name.slice(0, 18) + '…' : n.name}
                </text>
                {st?.running && (
                  <circle cx={x + NODE_W - 10} cy={y + 10} r="4" fill="#3b82f6">
                    <animate attributeName="opacity" values="1;0.3;1" dur="1.5s" repeatCount="indefinite"/>
                  </circle>
                )}
              </g>
            )
          })}
        </svg>
      </div>
      {ctxMenuEl}
    </div>
  )
}

// ─── Config editor ────────────────────────────────────────────────────────────

/** Minimal Myers-style line diff — returns [{type:'same'|'add'|'remove', line}] */
function diffLines(oldTxt, newTxt) {
  const a = oldTxt.split('\n')
  const b = newTxt.split('\n')
  const m = a.length, n = b.length
  // LCS length table
  const dp = Array.from({ length: m + 1 }, () => new Array(n + 1).fill(0))
  for (let i = m - 1; i >= 0; i--)
    for (let j = n - 1; j >= 0; j--)
      dp[i][j] = a[i] === b[j] ? dp[i + 1][j + 1] + 1 : Math.max(dp[i + 1][j], dp[i][j + 1])
  const result = []
  let i = 0, j = 0
  while (i < m || j < n) {
    if (i < m && j < n && a[i] === b[j]) { result.push({ type: 'same', line: a[i] }); i++; j++ }
    else if (j < n && (i >= m || dp[i][j + 1] >= dp[i + 1][j])) { result.push({ type: 'add', line: b[j] }); j++ }
    else { result.push({ type: 'remove', line: a[i] }); i++ }
  }
  return result
}

function ConfigView({ showToast }) {
  const { data, loading, reload } = useApi('/api/config', 0)
  const [editing,    setEditing]   = useState(false)
  const [yamlText,   setYamlText]  = useState('')
  const [valResult,  setValResult] = useState(null)
  const [saving,     setSaving]    = useState(false)
  const [diffResult, setDiffResult] = useState(null)
  const [errorLine,  setErrorLine]  = useState(null)
  const textareaRef  = useRef(null)
  const prevYamlRef  = useRef('')
  const [cfgTab, setCfgTab] = useState('jobs')
  const { data: daemonData, error: daemonError, loading: daemonLoading } = useApi('/api/config/daemon', 0)

  useEffect(() => {
    if (data?.yaml && !editing) setYamlText(data.yaml)
  }, [data?.yaml])

  // Parse "line N:" pattern from error strings
  function parseErrorLine(msg) {
    if (!msg) return null
    const m = msg.match(/line\s+(\d+)/i)
    return m ? parseInt(m[1], 10) : null
  }

  // Scroll textarea to a given 1-based line number
  function scrollToLine(lineNo) {
    const ta = textareaRef.current
    if (!ta || !lineNo) return
    const lines = ta.value.split('\n')
    let offset = 0
    for (let i = 0; i < Math.min(lineNo - 1, lines.length); i++) offset += lines[i].length + 1
    ta.focus()
    ta.setSelectionRange(offset, offset + (lines[lineNo - 1] ?? '').length)
    // Approximate scroll: measure line height
    const lineH = ta.scrollHeight / Math.max(lines.length, 1)
    ta.scrollTop = Math.max(0, (lineNo - 3) * lineH)
  }

  function handleEdit(text) {
    setYamlText(text)
    setDiffResult(null)
    setErrorLine(null)
    setValResult(null)
  }

  async function validate() {
    setValResult(null)
    const r    = await apiPost('/api/config/validate', { yaml: yamlText })
    const body = await r.json()
    setValResult(body)
    if (!body.ok) {
      const ln = parseErrorLine(body.error)
      setErrorLine(ln)
      if (ln) setTimeout(() => scrollToLine(ln), 50)
    } else {
      setErrorLine(null)
    }
  }

  async function save() {
    prevYamlRef.current = yamlText
    setSaving(true)
    setValResult(null)
    setDiffResult(null)
    const r    = await apiPost('/api/config/save', { yaml: yamlText })
    const body = await r.json()
    setSaving(false)
    if (body.ok) {
      const diff = diffLines(prevYamlRef.current, yamlText)
      const hasChange = diff.some(d => d.type !== 'same')
      if (hasChange) setDiffResult(diff)
      showToast('Config saved and reloaded', 'ok')
      setEditing(false)
      setErrorLine(null)
      reload()
    } else {
      setValResult(body)
      const ln = parseErrorLine(body.error)
      setErrorLine(ln)
      if (ln) setTimeout(() => scrollToLine(ln), 50)
      showToast('Save failed: ' + (body.error ?? 'unknown error'), 'error')
    }
  }

  return (
    <div class="p-5 space-y-4">
      {/* Config file subtabs */}
      <div class="flex gap-1 border-b border-gray-200 pb-0">
        {[['jobs', 'husky.yaml'], ['daemon', 'huskyd.yaml']].map(([id, label]) => (
          <button
            key={id}
            onClick={() => setCfgTab(id)}
            class={`px-3 py-1.5 text-xs font-medium rounded-t-md border border-b-0 transition-colors ${
              cfgTab === id
                ? 'bg-white border-gray-200 text-gray-900 -mb-px z-10 relative'
                : 'bg-gray-50 border-transparent text-gray-500 hover:text-gray-700 hover:bg-gray-100'
            }`}
          >{label}</button>
        ))}
      </div>

      {cfgTab === 'jobs' && <div class="rounded-xl border border-gray-200 bg-white shadow-sm overflow-hidden">
        <div class="flex items-center justify-between px-5 py-3 border-b border-gray-100 bg-gray-50">
          <div>
            <span class="text-sm font-semibold text-gray-700">husky.yaml</span>
            {data?.path && <span class="ml-2 text-xs font-mono text-gray-400">{data.path}</span>}
          </div>
          <div class="flex items-center gap-2">
            {!editing ? (
              <button
                onClick={() => { setEditing(true); setDiffResult(null) }}
                class="flex items-center gap-1.5 rounded-md border border-gray-300 bg-white px-3 py-1.5 text-xs font-medium text-gray-700 hover:bg-gray-50 transition-colors"
              >
                <EditIcon className="w-3.5 h-3.5" /> Edit
              </button>
            ) : (
              <>
                <button
                  onClick={validate}
                  class="flex items-center gap-1.5 rounded-md border border-yellow-300 bg-yellow-50 px-3 py-1.5 text-xs font-medium text-yellow-700 hover:bg-yellow-100 transition-colors"
                >
                  <CheckIcon className="w-3.5 h-3.5" /> Validate
                </button>
                <button
                  onClick={save}
                  disabled={saving}
                  class="flex items-center gap-1.5 rounded-md border border-green-300 bg-green-50 px-3 py-1.5 text-xs font-medium text-green-700 hover:bg-green-100 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                >
                  <SaveIcon className="w-3.5 h-3.5" /> {saving ? 'Saving…' : 'Save & Reload'}
                </button>
                <button
                  onClick={() => { setEditing(false); setYamlText(data?.yaml ?? ''); setValResult(null); setErrorLine(null) }}
                  class="text-gray-400 hover:text-gray-600 p-1 flex items-center justify-center rounded-full hover:bg-gray-100 ml-1"
                >
                  <XIcon className="w-4 h-4" />
                </button>
              </>
            )}
          </div>
        </div>

        {loading ? (
          <div class="flex items-center gap-2 p-6 text-sm text-gray-400"><Spinner />Loading config…</div>
        ) : editing ? (
          <div class="relative">
            {/* Line-number gutter annotation for error line */}
            {errorLine && (
              <div class="bg-red-950 border-b border-red-800 px-4 py-1.5 flex items-center gap-2 text-xs text-red-300">
                <AlertIcon className="w-3.5 h-3.5 flex-shrink-0" />
                <span>Error at line {errorLine} —&nbsp;</span>
                <button
                  onClick={() => scrollToLine(errorLine)}
                  class="underline hover:text-red-100"
                >jump to line</button>
              </div>
            )}
            <textarea
              ref={textareaRef}
              value={yamlText}
              onInput={e => handleEdit(e.target.value)}
              class={`w-full font-mono text-xs p-4 focus:outline-none resize-none bg-gray-950 text-gray-100 leading-relaxed ${errorLine ? 'border-l-2 border-red-500' : ''}`}
              rows={Math.max(20, (yamlText || '').split('\n').length + 2)}
              spellcheck={false}
            />
          </div>
        ) : (
          <pre class="text-xs font-mono text-gray-700 p-5 overflow-x-auto leading-relaxed whitespace-pre bg-gray-50 max-h-[70vh] overflow-y-auto">
            {data?.yaml ?? ''}
          </pre>
        )}

        {valResult && (
          <div class={`px-5 py-3 text-xs border-t flex items-center gap-1.5 ${valResult.ok ? 'bg-green-50 text-green-700 border-green-100' : 'bg-red-50 text-red-700 border-red-100'}`}>
            {valResult.ok
              ? <><CheckIcon className="w-4 h-4" /> Config is valid</>
              : <><XIcon className="w-4 h-4" /> {valResult.error}</>}
          </div>
        )}
      </div>}

      {/* huskyd.yaml tab — read-only daemon runtime config */}
      {cfgTab === 'daemon' && (
        <div class="rounded-xl border border-gray-200 bg-white shadow-sm overflow-hidden">
          <div class="px-5 py-3 border-b border-gray-100 bg-gray-50">
            <span class="text-sm font-semibold text-gray-700">huskyd.yaml</span>
            {daemonData?.path && <span class="ml-2 text-xs font-mono text-gray-400">{daemonData.path}</span>}
          </div>
          {daemonLoading ? (
            <div class="flex items-center gap-2 p-6 text-sm text-gray-400"><Spinner />Loading…</div>
          ) : daemonError ? (
            <p class="px-5 py-6 text-sm text-gray-400 italic">
              No <code class="font-mono">huskyd.yaml</code> found — daemon is using all defaults.
            </p>
          ) : (
            <pre class="text-xs font-mono text-gray-700 p-5 overflow-x-auto leading-relaxed whitespace-pre bg-gray-50 max-h-[70vh] overflow-y-auto">
              {daemonData?.yaml ?? ''}
            </pre>
          )}
        </div>
      )}

      {/* Diff panel — shown after a successful save */}
      {cfgTab === 'jobs' && diffResult && (
        <div class="rounded-xl border border-gray-200 bg-white shadow-sm overflow-hidden">
          <div class="flex items-center justify-between px-5 py-2.5 border-b border-gray-100 bg-gray-50">
            <span class="text-xs font-semibold text-gray-600">Changes applied</span>
            <button onClick={() => setDiffResult(null)} class="text-gray-400 hover:text-gray-600 p-0.5 rounded">
              <XIcon className="w-4 h-4" />
            </button>
          </div>
          <div class="overflow-x-auto max-h-80 overflow-y-auto font-mono text-xs leading-relaxed">
            {(() => {
              // Collapse long same-runs, show up to 2 context lines around changes
              const CONTEXT = 2
              const out = []
              let i = 0
              while (i < diffResult.length) {
                const d = diffResult[i]
                if (d.type === 'same') {
                  // check if within CONTEXT lines of a change
                  const nearBefore = diffResult.slice(Math.max(0, i - CONTEXT), i).some(x => x.type !== 'same')
                  const nearAfter  = diffResult.slice(i + 1, i + 1 + CONTEXT).some(x => x.type !== 'same')
                  if (nearBefore || nearAfter) {
                    out.push(<div class="px-4 py-px bg-white text-gray-400">&nbsp;{d.line}</div>)
                  } else {
                    // skip, but add ellipsis marker once
                    if (out[out.length - 1]?.key !== 'ellipsis') {
                      out.push(<div key="ellipsis" class="px-4 py-0.5 text-gray-300 italic select-none bg-gray-50">⋯</div>)
                    }
                  }
                } else if (d.type === 'add') {
                  out.push(<div class="px-4 py-px bg-green-50 text-green-800">+&nbsp;{d.line}</div>)
                } else {
                  out.push(<div class="px-4 py-px bg-red-50 text-red-700">−&nbsp;{d.line}</div>)
                }
                i++
              }
              return out
            })()}
          </div>
        </div>
      )}
    </div>
  )
}

// ─── Health & SLA view ───────────────────────────────────────────────────────
function HealthView({ onNavigate }) {
  const { data: jobs }    = useApi('/api/jobs', 30000)
  const [runHistory, setRunHistory] = useState([])

  // Fetch recent runs for SLA calculations
  useEffect(() => {
    fetch('/api/db/job_runs?limit=400')
      .then(r => r.json()).then(setRunHistory).catch(() => {})
  }, [])

  const slaJobs = (jobs ?? []).filter(j => j.sla)
  const statusMap = {}
  for (const j of jobs ?? []) statusMap[j.name] = j

  // Per-job SLA stats
  const slaRows = slaJobs.map(j => {
    const slaS = goDurSec(j.sla)
    const jobRuns = runHistory.filter(r => r.job_name === j.name && r.finished_at)
    const lastRun = jobRuns[0]
    const lastDur = lastRun ? durSec(lastRun.started_at, lastRun.finished_at) : null
    const breached = jobRuns.filter(r => r.sla_breached).length
    const rate = jobRuns.length > 0 ? Math.round((breached / jobRuns.length) * 100) : null
    const delta = lastDur != null ? lastDur - slaS : null
    return { name: j.name, sla: j.sla, slaS, lastDur, delta, breached, total: jobRuns.length, rate }
  })

  // Timeline data: last 50 runs per visible job, for swimlane
  const timelineJobs = (jobs ?? []).slice(0, 12)
  const minTs = runHistory.length
    ? Math.min(...runHistory.filter(r => r.started_at).map(r => new Date(r.started_at).getTime()))
    : Date.now() - 86400000
  const maxTs = Date.now()
  const span  = maxTs - minTs || 1

  return (
    <div class="p-5 space-y-6">
      {/* SLA compliance table */}
      <div class="rounded-xl border border-gray-200 bg-white shadow-sm overflow-hidden">
        <div class="px-5 py-3 bg-gray-50 border-b border-gray-100">
          <h2 class="text-sm font-semibold text-gray-700">SLA Compliance</h2>
          <p class="text-xs text-gray-400 mt-0.5">Jobs with SLA budgets configured</p>
        </div>
        {slaJobs.length === 0 ? (
          <p class="px-5 py-6 text-sm text-gray-400">No jobs have SLA configured.</p>
        ) : (
          <table class="min-w-full divide-y divide-gray-200 text-sm">
            <thead class="bg-gray-50">
              <tr>
                {['Job','SLA budget','Last duration','Δ','Breach rate','Breaches'].map(h => <Th key={h}>{h}</Th>)}
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-100">
              {slaRows.map(row => (
                <tr key={row.name} class="hover:bg-gray-50">
                  <td class="px-4 py-3 font-mono font-medium text-gray-900">{row.name}</td>
                  <td class="px-4 py-3 font-mono text-gray-600">{row.sla}</td>
                  <td class="px-4 py-3 font-mono text-gray-600">{row.lastDur != null ? `${row.lastDur}s` : '—'}</td>
                  <td class="px-4 py-3 font-mono">
                    {row.delta == null ? <span class="text-gray-300">—</span>
                      : row.delta > 0
                      ? <span class="text-red-600 font-semibold">+{row.delta}s</span>
                      : <span class="text-green-600">{row.delta}s</span>
                    }
                  </td>
                  <td class="px-4 py-3">
                    {row.rate == null ? <span class="text-gray-300">—</span> : (
                      <div class="flex items-center gap-2">
                        <div class="h-1.5 w-16 rounded-full bg-gray-100">
                          <div class={`h-1.5 rounded-full ${row.rate > 20 ? 'bg-red-500' : row.rate > 5 ? 'bg-amber-500' : 'bg-green-500'}`}
                            style={`width:${Math.min(100, row.rate)}%`} />
                        </div>
                        <span class={`text-xs font-medium ${row.rate > 20 ? 'text-red-600' : row.rate > 5 ? 'text-amber-600' : 'text-green-600'}`}>{row.rate}%</span>
                      </div>
                    )}
                  </td>
                  <td class="px-4 py-3 text-xs text-gray-500">{row.breached}/{row.total}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      {/* Timeline swimlane */}
      <div class="rounded-xl border border-gray-200 bg-white shadow-sm overflow-hidden">
        <div class="px-5 py-3 bg-gray-50 border-b border-gray-100">
          <h2 class="text-sm font-semibold text-gray-700">Run Timeline</h2>
          <p class="text-xs text-gray-400 mt-0.5">Recent run history per job (last 24h view)</p>
        </div>
        <div class="p-5 overflow-x-auto">
          <div class="min-w-[600px]">
            {/* Timeline Axis Header */}
            {timelineJobs.length > 0 && (
              <div class="flex items-center gap-4 mb-3">
                <div class="w-36 shrink-0"></div>
                <div class="relative flex-1 flex justify-between text-xs font-medium text-gray-400">
                  <span>{new Date(minTs).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}</span>
                  <span>{new Date(minTs + span / 2).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}</span>
                  <span>{new Date(maxTs).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}</span>
                </div>
              </div>
            )}

            <div class="relative space-y-3">
              {/* Background vertical grid lines */}
              {timelineJobs.length > 0 && (
                <div class="absolute inset-0 flex ml-40 pointer-events-none">
                  <div class="w-full border-l border-r border-gray-100 flex justify-center mt-1 mb-1">
                    <div class="border-l border-gray-100 h-full"></div>
                  </div>
                </div>
              )}

              {timelineJobs.map(j => {
                const jRuns = runHistory.filter(r => r.job_name === j.name && r.started_at)
                return (
                  <div key={j.name} class="relative flex items-center gap-4 z-10">
                    <span class="text-sm font-medium text-gray-700 w-36 shrink-0 truncate text-right" title={j.name}>{j.name}</span>
                    <div class="relative flex-1 h-8 bg-gray-50/50 rounded-md border border-gray-100 shadow-inner">
                      {jRuns.map(r => {
                        const start = new Date(r.started_at).getTime()
                        const end   = r.finished_at ? new Date(r.finished_at).getTime() : Date.now()
                        const left  = ((start - minTs) / span) * 100
                        const width = Math.max(0.4, ((end - start) / span) * 100)
                        
                        const colors = {
                          SUCCESS: 'bg-emerald-500 hover:bg-emerald-400 ring-emerald-600',
                          FAILED:  'bg-rose-500 hover:bg-rose-400 ring-rose-600',
                          RUNNING: 'bg-blue-500 hover:bg-blue-400 ring-blue-600 animate-pulse',
                        }
                        const colorClass = colors[r.status] || 'bg-gray-400 hover:bg-gray-300 ring-gray-500'

                        return (
                          <div
                            key={r.id}
                            class={`absolute top-1 bottom-1 rounded-sm ${colorClass} ring-1 ring-inset ring-opacity-20 shadow-sm cursor-pointer min-w-[4px] transition-all duration-200 hover:-translate-y-0.5 hover:shadow-md z-20 group`}
                            style={`left:${left}%;width:${width}%`}
                            onClick={() => onNavigate && onNavigate(r.id)}
                          >
                            {/* Hover Tooltip */}
                            <div class="absolute bottom-full mb-2 left-1/2 -translate-x-1/2 opacity-0 group-hover:opacity-100 pointer-events-none z-50 bg-gray-900 text-white text-xs rounded py-1 px-2 whitespace-nowrap shadow-xl transition-opacity">
                              <span class="font-bold">#{r.id}</span> &middot; {r.status}
                              <div class="mt-0.5 text-gray-300">{fmtTs(r.started_at)}</div>
                              <div class="absolute -bottom-1 left-1/2 -translate-x-1/2 w-2 h-2 bg-gray-900 rotate-45"></div>
                            </div>
                          </div>
                        )
                      })}
                    </div>
                  </div>
                )
              })}
              {timelineJobs.length === 0 && (
                <div class="py-8 text-center text-sm text-gray-400 border border-dashed border-gray-200 rounded-lg bg-gray-50">
                  No run data available.
                </div>
              )}
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}

// ─── Integrations view ────────────────────────────────────────────────────────
function IntegrationsView({ showToast }) {
  const { data: integrations, loading, reload } = useApi('/api/integrations', 30000)
  const [testResults, setTestResults] = useState({})
  const [testing, setTesting]         = useState({})
  const list = integrations ?? []

  async function testIntegration(name) {
    setTesting(t => ({ ...t, [name]: true }))
    try {
      const r    = await apiPost(`/api/integrations/${encodeURIComponent(name)}/test`)
      const body = await r.json().catch(() => ({}))
      setTestResults(prev => ({ ...prev, [name]: { ok: body.ok ?? r.ok, error: body.error } }))
      showToast(body.ok || r.ok ? `${name}: delivery OK` : `${name}: ${body.error ?? 'test failed'}`, body.ok || r.ok ? 'ok' : 'error')
    } catch (e) {
      setTestResults(prev => ({ ...prev, [name]: { ok: false, error: e.message } }))
      showToast(`${name}: ${e.message}`, 'error')
    } finally {
      setTesting(t => ({ ...t, [name]: false }))
    }
  }

  return (
    <div class="p-5 space-y-4">
      <div class="rounded-xl border border-gray-200 bg-white shadow-sm overflow-hidden">
        <div class="flex items-center justify-between px-5 py-3 bg-gray-50 border-b border-gray-100">
          <div>
            <h2 class="text-sm font-semibold text-gray-700">Integrations</h2>
            <p class="text-xs text-gray-400 mt-0.5">Notification channels configured for this daemon</p>
          </div>
          <button onClick={reload} class="flex items-center gap-1.5 text-xs text-gray-500 hover:text-gray-700 transition-colors">
            {loading ? <Spinner sm /> : <RefreshCwIcon className="w-3.5 h-3.5" />} Refresh
          </button>
        </div>
        {list.length === 0 ? (
          <div class="px-5 py-8 text-center">
            {loading ? <div class="flex items-center justify-center gap-2 text-gray-400"><Spinner />Loading…</div>
              : <p class="text-sm text-gray-400">No integrations configured. Add a <code class="font-mono text-gray-600">notify</code> section to your husky.yaml.</p>}
          </div>
        ) : (
          <table class="min-w-full divide-y divide-gray-200 text-sm">
            <thead class="bg-gray-50">
              <tr>
                {['Name','Provider','Credential status','Last test',''].map(h => <Th key={h}>{h}</Th>)}
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-100">
              {list.map(intg => {
                const res = testResults[intg.name]
                const configured = intg.status === 'configured'
                return (
                  <tr key={intg.name} class="hover:bg-gray-50">
                    <td class="px-4 py-3 font-mono font-medium text-gray-900">{intg.name}</td>
                    <td class="px-4 py-3 text-gray-600 capitalize">{intg.provider}</td>
                    <td class="px-4 py-3">
                      <span class={`inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-medium ring-1 ring-inset ${configured ? 'bg-green-50 text-green-700 ring-green-600/20' : 'bg-amber-50 text-amber-700 ring-amber-600/20'}`}>
                        <span class={`h-1.5 w-1.5 rounded-full ${configured ? 'bg-green-500' : 'bg-amber-400'}`} />
                        {configured ? 'configured' : intg.status || 'unknown'}
                      </span>
                    </td>
                    <td class="px-4 py-3">
                      {res == null
                        ? <span class="text-xs text-gray-400">—</span>
                        : res.ok
                        ? <span class="text-xs text-green-600 flex items-center gap-1"><CheckIcon className="w-3.5 h-3.5" /> OK</span>
                        : <span class="text-xs text-red-600 flex items-center gap-1 max-w-xs truncate" title={res.error}><XIcon className="w-3.5 h-3.5" /> {res.error || 'failed'}</span>
                      }
                    </td>
                    <td class="px-4 py-3 text-right">
                      <button
                        onClick={() => testIntegration(intg.name)}
                        disabled={testing[intg.name]}
                        class="flex items-center gap-1.5 rounded border border-blue-200 bg-blue-50 px-3 py-1.5 text-xs font-medium text-blue-700 hover:bg-blue-100 transition-colors disabled:opacity-50"
                      >
                        {testing[intg.name] ? <Spinner sm /> : <ActivityIcon className="w-3.5 h-3.5" />} Test
                      </button>
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        )}
      </div>
    </div>
  )
}

// ─── App root ─────────────────────────────────────────────────────────────────
function App() {
  const [tab, setTab]                          = useState('Jobs')
  const [runDetail, setRunDetail]              = useState(null)
  const [dagCycleId, setDagCycleId]            = useState(null)
  const [autoRefresh, setAutoRefresh]          = useState(true)
  const [lastRefreshed, setLastRefreshed]      = useState(Date.now())
  const { data: status, reload: reloadStatus } = useApi('/api/status', autoRefresh ? 15000 : 0)
  const { data: tags }                         = useApi('/api/tags', 60000)
  const { toast, show: showToast, hide: hideToast } = useToast()

  // Hash-based routing: #/runs/:id
  useEffect(() => {
    function onHash() {
      const hash = window.location.hash
      const runMatch = hash.match(/^#\/runs\/(\d+)$/)
      if (runMatch) { setRunDetail(parseInt(runMatch[1], 10)); setDagCycleId(null); return }
      const dagMatch = hash.match(/^#\/dag\?cycle=([^&]+)$/)
      if (dagMatch) { setRunDetail(null); setTab('DAG'); setDagCycleId(decodeURIComponent(dagMatch[1])); return }
      setRunDetail(null)
      setDagCycleId(null)
    }
    onHash()
    window.addEventListener('hashchange', onHash)
    return () => window.removeEventListener('hashchange', onHash)
  }, [])

  function navigateTo(runId) { window.location.hash = runId ? `#/runs/${runId}` : '#' }
  function navigateBack()    { window.location.hash = '#' }

  // Update last-refresh timestamp
  useEffect(() => { if (status) setLastRefreshed(Date.now()) }, [status])

  // Keyboard shortcuts: r = refresh, Esc = back from run detail
  useEffect(() => {
    function onKey(e) {
      if (e.target.tagName === 'INPUT' || e.target.tagName === 'TEXTAREA' || e.target.tagName === 'SELECT') return
      if (e.key === 'r' && !e.metaKey && !e.ctrlKey) { reloadStatus(); setLastRefreshed(Date.now()) }
      if (e.key === 'Escape') navigateBack()
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [])

  async function handleStop() {
    const r = await apiPost('/api/daemon/stop')
    showToast(r.ok ? 'Daemon stopping…' : 'Stop failed', r.ok ? 'ok' : 'error')
    if (!r.ok) {
      setTimeout(reloadStatus, 2000)
      return
    }
    setTimeout(() => window.location.reload(), 1200)
  }

  async function handleReload() {
    const r    = await apiPost('/api/daemon/reload')
    const body = await r.json().catch(() => ({}))
    showToast(body.ok ? 'Config reloaded' : 'Reload failed: ' + (body.error ?? ''), body.ok ? 'ok' : 'error')
  }

  // Run Detail full-page view
  if (runDetail !== null) {
    return (
      <div class="min-h-screen bg-gray-50 flex flex-col">
        <TopBar status={status} onStop={handleStop} onReload={handleReload} />
        <main class="flex-1 overflow-auto">
          <RunDetailView runId={runDetail} onBack={navigateBack} onNavigate={navigateTo} showToast={showToast} />
        </main>
        {toast && <Toast msg={toast.msg} type={toast.type} onClose={hideToast} />}
      </div>
    )
  }

  return (
    <div class="min-h-screen bg-gray-50 flex flex-col">
      <TopBar status={status} onStop={handleStop} onReload={handleReload} />
      <Tabs   active={tab} onChange={setTab} />
      <main class="flex-1 overflow-auto">
        {tab === 'Jobs'         && <JobsView tags={tags} showToast={showToast} onNavigate={navigateTo} />}
        {tab === 'Audit'        && <AuditView tags={tags} onNavigate={navigateTo} />}
        {tab === 'Outputs'      && <OutputsView />}
        {tab === 'Data'         && <DataView showToast={showToast} onNavigate={navigateTo} />}
        {tab === 'Alerts'       && <AlertsView showToast={showToast} />}
        {tab === 'DAG'          && <DagView onNavigate={navigateTo} showToast={showToast} cycleId={dagCycleId} onCycleDismiss={() => { setDagCycleId(null); if (window.location.hash.startsWith('#/dag')) window.location.hash = '#' }} />}
        {tab === 'Health'       && <HealthView onNavigate={navigateTo} />}
        {tab === 'Integrations' && <IntegrationsView showToast={showToast} />}
        {tab === 'Config'       && <ConfigView showToast={showToast} />}
      </main>
      <div class="fixed bottom-4 left-4 flex items-center gap-2 text-xs select-none pointer-events-none">
        <button
          onClick={() => setAutoRefresh(v => !v)}
          class={`pointer-events-auto flex items-center gap-1.5 rounded-full px-2.5 py-1 border text-xs transition-colors ${autoRefresh ? 'border-green-300 bg-green-50 text-green-700' : 'border-gray-300 bg-white text-gray-500'}`}
          title={autoRefresh ? 'Auto-refresh on — click to pause' : 'Auto-refresh paused — click to resume'}
        >
          <span class={`h-1.5 w-1.5 rounded-full ${autoRefresh ? 'bg-green-500 animate-pulse' : 'bg-gray-400'}`} />
          {autoRefresh ? 'Live' : 'Paused'}
        </button>
        <span class="text-gray-400">refreshed {fmtRel(new Date(lastRefreshed).toISOString())}</span>
      </div>
      {toast && <Toast msg={toast.msg} type={toast.type} onClose={hideToast} />}
    </div>
  )
}

render(<App />, document.getElementById('app'))
