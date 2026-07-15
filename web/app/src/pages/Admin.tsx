import { Plug, RefreshCw, Trash2, UserPlus } from 'lucide-react'
import { FormEvent, useCallback, useEffect, useState } from 'react'
import { api, Instance, User } from '../api'
import { useAuth } from '../App'

type Tab = 'instances' | 'users' | 'connections' | 'status'

export default function Admin() {
  const [tab, setTab] = useState<Tab>('instances')
  return (
    <div className="space-y-6 max-w-5xl">
      <h1 className="text-2xl font-bold">Admin</h1>
      <div className="flex gap-2">
        {(['instances', 'users', 'connections', 'status'] as Tab[]).map((t) => (
          <button
            key={t}
            onClick={() => setTab(t)}
            className={`btn capitalize ${tab === t ? 'bg-accent text-white' : 'bg-white/5 text-slate-300 hover:bg-white/10'}`}
          >
            {t}
          </button>
        ))}
      </div>
      {tab === 'instances' && <Instances />}
      {tab === 'users' && <Users />}
      {tab === 'connections' && (
        <div className="space-y-4">
          <LastfmConfig />
          <PlexConfig />
        </div>
      )}
      {tab === 'status' && <Status />}
    </div>
  )
}

// ---------------- instances ----------------

interface LidarrOptions {
  qualityProfiles: { id: number; name: string }[]
  metadataProfiles: { id: number; name: string }[]
  rootFolders: string[]
}

function Instances() {
  const [items, setItems] = useState<Instance[]>([])
  const [editing, setEditing] = useState<Partial<Instance> & { apiKey?: string } | null>(null)
  const [testResult, setTestResult] = useState('')
  const [error, setError] = useState('')
  const [options, setOptions] = useState<LidarrOptions | null>(null)

  const load = useCallback(() => api.get<Instance[]>('/api/instances').then(setItems), [])
  useEffect(() => {
    load()
  }, [load])

  const startEdit = async (inst?: Instance) => {
    setTestResult('')
    setError('')
    setOptions(null)
    setEditing(inst ? { ...inst, apiKey: '' } : { type: 'navidrome', isActive: true })
    if (inst?.type === 'lidarr') {
      api.get<LidarrOptions>(`/api/instances/${inst.id}/lidarr-options`).then(setOptions).catch(() => {})
    }
  }

  const test = async () => {
    if (!editing) return
    setTestResult('testing…')
    try {
      const res = await api.post<{ ok: boolean; version?: string }>('/api/instances/test', {
        type: editing.type,
        baseUrl: editing.baseUrl,
        username: editing.username ?? '',
        apiKey: editing.apiKey ?? '',
      })
      setTestResult(`✓ connected${res.version ? ` (Lidarr ${res.version})` : ''}`)
    } catch (e) {
      setTestResult(`✗ ${e instanceof Error ? e.message : 'failed'}`)
    }
  }

  const save = async (e: FormEvent) => {
    e.preventDefault()
    if (!editing) return
    setError('')
    const body = {
      name: editing.name ?? '',
      type: editing.type,
      baseUrl: editing.baseUrl ?? '',
      username: editing.username ?? '',
      apiKey: editing.apiKey ?? '',
      isActive: editing.isActive ?? true,
      isAuthSource: editing.isAuthSource ?? false,
      qualityProfileId: Number(editing.qualityProfileId) || 0,
      metadataProfileId: Number(editing.metadataProfileId) || 0,
      rootFolder: editing.rootFolder ?? '',
    }
    try {
      if (editing.id) await api.put(`/api/instances/${editing.id}`, body)
      else await api.post('/api/instances', body)
      setEditing(null)
      load()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'save failed')
    }
  }

  const remove = async (id: number) => {
    if (!confirm('Delete this instance?')) return
    await api.del(`/api/instances/${id}`)
    load()
  }

  return (
    <div className="space-y-4">
      <div className="flex justify-end">
        <button className="btn-primary" onClick={() => startEdit()}>
          <Plug size={15} /> Add instance
        </button>
      </div>

      <div className="card divide-y divide-white/5">
        {items.length === 0 && (
          <div className="p-8 text-center text-sm text-slate-500 space-y-1">
            <p>No instances configured yet.</p>
            <p>Add your Lidarr server (fulfils requests and doubles as the library source for recommendations).</p>
            <p>Navidrome is optional — add it for richer library signals (stars/ratings) and Navidrome-credential logins.</p>
          </div>
        )}
        {items.map((i) => (
          <div key={i.id} className="flex items-center gap-4 p-4">
            <div className="min-w-0 flex-1">
              <div className="font-semibold text-sm">
                {i.name}{' '}
                <span className="text-xs font-normal text-slate-500 uppercase ml-1">{i.type}</span>
                {i.isAuthSource && <span className="text-xs text-accent ml-2">auth source</span>}
                {!i.isActive && <span className="text-xs text-amber-400 ml-2">disabled</span>}
              </div>
              <div className="text-xs text-slate-500 truncate">{i.baseUrl}</div>
            </div>
            <button className="btn-ghost" onClick={() => startEdit(i)}>Edit</button>
            <button className="btn-ghost !px-2.5" onClick={() => remove(i.id)}>
              <Trash2 size={15} className="text-slate-500" />
            </button>
          </div>
        ))}
      </div>

      {editing && (
        <form onSubmit={save} className="card p-6 space-y-3">
          <h3 className="font-bold">{editing.id ? `Edit ${editing.name}` : 'New instance'}</h3>
          {!editing.id && (
            <select
              className="input"
              value={editing.type}
              onChange={(e) => setEditing({ ...editing, type: e.target.value as Instance['type'] })}
            >
              <option value="navidrome">Navidrome (optional — library + login)</option>
              <option value="lidarr">Lidarr (requests + library source)</option>
            </select>
          )}
          <input className="input" placeholder="Display name" value={editing.name ?? ''} onChange={(e) => setEditing({ ...editing, name: e.target.value })} required />
          <input className="input" placeholder="Base URL, e.g. http://10.0.10.249:4533" value={editing.baseUrl ?? ''} onChange={(e) => setEditing({ ...editing, baseUrl: e.target.value })} required />
          {editing.type === 'navidrome' && (
            <input className="input" placeholder="Navidrome username" value={editing.username ?? ''} onChange={(e) => setEditing({ ...editing, username: e.target.value })} required />
          )}
          <input
            className="input"
            type="password"
            placeholder={
              editing.id
                ? 'New password / API key (leave blank to keep current)'
                : editing.type === 'navidrome'
                  ? 'Navidrome password'
                  : 'Lidarr API key'
            }
            value={editing.apiKey ?? ''}
            onChange={(e) => setEditing({ ...editing, apiKey: e.target.value })}
            required={!editing.id}
          />
          {editing.type === 'lidarr' && (
            <div className="grid sm:grid-cols-3 gap-3">
              <SelectOrInput
                placeholder="Quality profile ID"
                value={editing.qualityProfileId}
                options={options?.qualityProfiles.map((p) => ({ value: p.id, label: p.name }))}
                onChange={(v) => setEditing({ ...editing, qualityProfileId: Number(v) })}
              />
              <SelectOrInput
                placeholder="Metadata profile ID"
                value={editing.metadataProfileId}
                options={options?.metadataProfiles.map((p) => ({ value: p.id, label: p.name }))}
                onChange={(v) => setEditing({ ...editing, metadataProfileId: Number(v) })}
              />
              <SelectOrInput
                placeholder="Root folder, e.g. /music"
                value={editing.rootFolder}
                options={options?.rootFolders.map((r) => ({ value: r, label: r }))}
                onChange={(v) => setEditing({ ...editing, rootFolder: String(v) })}
                text
              />
            </div>
          )}
          {editing.type === 'navidrome' && (
            <label className="flex items-center gap-2 text-sm text-slate-400">
              <input type="checkbox" checked={editing.isAuthSource ?? false} onChange={(e) => setEditing({ ...editing, isAuthSource: e.target.checked })} />
              Use as login source (members sign in with Navidrome credentials)
            </label>
          )}
          <label className="flex items-center gap-2 text-sm text-slate-400">
            <input type="checkbox" checked={editing.isActive ?? true} onChange={(e) => setEditing({ ...editing, isActive: e.target.checked })} />
            Active
          </label>
          {testResult && <p className="text-sm text-slate-400">{testResult}</p>}
          {error && <p className="text-sm text-red-400">{error}</p>}
          <div className="flex gap-2">
            <button type="button" className="btn-ghost" onClick={test}>Test connection</button>
            <button className="btn-primary">Save</button>
            <button type="button" className="btn-ghost" onClick={() => setEditing(null)}>Cancel</button>
          </div>
        </form>
      )}
    </div>
  )
}

function SelectOrInput({
  placeholder,
  value,
  options,
  onChange,
  text,
}: {
  placeholder: string
  value: unknown
  options?: { value: string | number; label: string }[]
  onChange: (v: string | number) => void
  text?: boolean
}) {
  if (options && options.length > 0) {
    return (
      <select className="input" value={String(value ?? '')} onChange={(e) => onChange(e.target.value)}>
        <option value="">{placeholder}</option>
        {options.map((o) => (
          <option key={String(o.value)} value={String(o.value)}>
            {o.label}
          </option>
        ))}
      </select>
    )
  }
  return (
    <input
      className="input"
      type={text ? 'text' : 'number'}
      placeholder={placeholder}
      value={value == null || value === 0 ? '' : String(value)}
      onChange={(e) => onChange(e.target.value)}
    />
  )
}

// ---------------- users ----------------

function Users() {
  const { user: me } = useAuth()
  const [items, setItems] = useState<User[]>([])
  const [adding, setAdding] = useState(false)
  const [form, setForm] = useState({ username: '', email: '', password: '', role: 'user', canAutoApprove: false })
  const [error, setError] = useState('')

  const load = useCallback(() => api.get<User[]>('/api/users').then(setItems), [])
  useEffect(() => {
    load()
  }, [load])

  const create = async (e: FormEvent) => {
    e.preventDefault()
    setError('')
    try {
      await api.post('/api/users', form)
      setAdding(false)
      setForm({ username: '', email: '', password: '', role: 'user', canAutoApprove: false })
      load()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'failed')
    }
  }

  const toggleAutoApprove = async (u: User) => {
    await api.put(`/api/users/${u.id}`, { canAutoApprove: !u.canAutoApprove })
    load()
  }

  const setLastfm = async (u: User) => {
    const current = u.lastfmUser ?? ''
    const value = prompt(
      `Last.fm username for ${u.username}\n\nRecommendations will follow what this Last.fm account actually listens to (top artists, last 3 months). Leave empty to unlink.`,
      current,
    )
    if (value === null || value === current) return
    await api.put(`/api/users/${u.id}`, { lastfmUser: value.trim() })
    load()
  }

  const remove = async (u: User) => {
    if (!confirm(`Delete user ${u.username}? Their requests are removed too.`)) return
    await api.del(`/api/users/${u.id}`)
    load()
  }

  return (
    <div className="space-y-4">
      <div className="flex justify-end">
        <button className="btn-primary" onClick={() => setAdding(!adding)}>
          <UserPlus size={15} /> Add user
        </button>
      </div>
      {adding && (
        <form onSubmit={create} className="card p-6 space-y-3">
          <input className="input" placeholder="Username" value={form.username} onChange={(e) => setForm({ ...form, username: e.target.value })} required />
          <input className="input" type="email" placeholder="Email (optional)" value={form.email} onChange={(e) => setForm({ ...form, email: e.target.value })} />
          <input className="input" type="password" placeholder="Password (blank = signs in via Navidrome)" value={form.password} onChange={(e) => setForm({ ...form, password: e.target.value })} />
          <div className="flex items-center gap-6">
            <label className="flex items-center gap-2 text-sm text-slate-400">
              <input type="checkbox" checked={form.role === 'admin'} onChange={(e) => setForm({ ...form, role: e.target.checked ? 'admin' : 'user' })} />
              Admin
            </label>
            <label className="flex items-center gap-2 text-sm text-slate-400">
              <input type="checkbox" checked={form.canAutoApprove} onChange={(e) => setForm({ ...form, canAutoApprove: e.target.checked })} />
              Auto-approve requests
            </label>
          </div>
          {error && <p className="text-sm text-red-400">{error}</p>}
          <button className="btn-primary">Create</button>
        </form>
      )}
      <div className="card divide-y divide-white/5">
        {items.map((u) => (
          <div key={u.id} className="flex items-center gap-4 p-4">
            <div className="min-w-0 flex-1">
              <div className="font-semibold text-sm">
                {u.username}
                {u.role === 'admin' && <span className="text-xs text-accent ml-2">admin</span>}
              </div>
              <div className="text-xs text-slate-500">{u.email || 'no email'}</div>
            </div>
            <button
              className="text-xs text-slate-400 hover:text-accent"
              onClick={() => setLastfm(u)}
              title="Link a Last.fm account — recommendations follow its listening history"
            >
              {u.lastfmUser ? `♫ ${u.lastfmUser}` : '♫ link Last.fm'}
            </button>
            <label className="flex items-center gap-2 text-xs text-slate-400">
              <input type="checkbox" checked={u.canAutoApprove} onChange={() => toggleAutoApprove(u)} />
              auto-approve
            </label>
            {u.id !== me?.id && (
              <button className="btn-ghost !px-2.5" onClick={() => remove(u)}>
                <Trash2 size={15} className="text-slate-500" />
              </button>
            )}
          </div>
        ))}
      </div>
    </div>
  )
}

// ---------------- last.fm ----------------

function LastfmConfig() {
  const [config, setConfig] = useState<{ configured: boolean; source: string } | null>(null)
  const [key, setKey] = useState('')
  const [busy, setBusy] = useState(false)
  const [message, setMessage] = useState('')

  const load = useCallback(() => api.get<{ configured: boolean; source: string }>('/api/admin/lastfm').then(setConfig), [])
  useEffect(() => {
    load()
  }, [load])

  const save = async (apiKey: string) => {
    setBusy(true)
    setMessage('')
    try {
      await api.post('/api/admin/lastfm', { apiKey })
      setKey('')
      setMessage(
        apiKey
          ? 'Key validated and saved — discovery switched to Last.fm. Trending is resyncing and recommendations will rebuild in the background.'
          : 'Key removed — discovery switched back to the keyless backends.',
      )
      load()
    } catch (e) {
      setMessage(e instanceof Error ? e.message : 'failed')
    } finally {
      setBusy(false)
    }
  }

  if (!config) return <p className="text-sm text-slate-500">Loading…</p>

  return (
    <div className="card p-6 space-y-3 max-w-2xl">
      <h3 className="font-bold">Last.fm</h3>
      <p className="text-sm text-slate-400">
        Without a key, discovery runs on the keyless Deezer / ListenBrainz / MusicBrainz backends.
        A free Last.fm API key upgrades trending, similar-artist data and search to Last.fm's richer
        listening graph — paste it here whenever you manage to create an account
        (<a className="text-accent hover:underline" href="https://www.last.fm/api/account/create" target="_blank" rel="noreferrer">last.fm/api/account/create</a>).
        The key is validated live, stored encrypted, and applied without a restart.
      </p>
      <div className="text-sm">
        <span className="text-slate-400">Status:</span>{' '}
        {config.configured ? (
          <span className="text-emerald-400 font-semibold">
            active{config.source === 'env' ? ' (from environment variable)' : ' (configured here)'}
          </span>
        ) : (
          <span className="text-slate-300">keyless mode</span>
        )}
      </div>
      <div className="flex gap-2">
        <input
          className="input flex-1"
          type="password"
          placeholder="Last.fm API key"
          value={key}
          onChange={(e) => setKey(e.target.value)}
        />
        <button className="btn-primary" disabled={!key.trim() || busy} onClick={() => save(key.trim())}>
          {busy ? 'Validating…' : 'Validate & save'}
        </button>
        {config.source === 'admin' && (
          <button className="btn-ghost" disabled={busy} onClick={() => save('')}>
            Remove
          </button>
        )}
      </div>
      {message && <p className="text-sm text-slate-400">{message}</p>}
    </div>
  )
}

// ---------------- plex ----------------

interface PlexServerOption {
  name: string
  machineIdentifier: string
  owned: boolean
}

function PlexConfig() {
  const [config, setConfig] = useState<{ enabled: boolean; machineId: string; serverName: string } | null>(null)
  const [servers, setServers] = useState<PlexServerOption[] | null>(null)
  const [busy, setBusy] = useState(false)
  const [message, setMessage] = useState('')

  const load = useCallback(() => api.get<{ enabled: boolean; machineId: string; serverName: string }>('/api/admin/plex').then(setConfig), [])
  useEffect(() => {
    load()
  }, [load])

  const connect = async () => {
    setBusy(true)
    setMessage('A Plex window opened — approve the link there…')
    setServers(null)
    try {
      const start = await api.post<{ pinId: number; authUrl: string }>('/api/auth/plex/start')
      window.open(start.authUrl, '_blank', 'noopener,width=600,height=700')
      const poll = async (): Promise<void> => {
        const res = await api.post<{ pending?: boolean; servers?: PlexServerOption[] }>(
          '/api/auth/plex/poll?setup=1',
          { pinId: start.pinId },
        )
        if (res.servers) {
          setServers(res.servers)
          setMessage(res.servers.length ? 'Pick the server whose members may sign in:' : 'No Plex servers found on that account.')
          setBusy(false)
          return
        }
        window.setTimeout(poll, 2000)
      }
      poll()
    } catch (e) {
      setMessage(e instanceof Error ? e.message : 'failed')
      setBusy(false)
    }
  }

  const choose = async (srv: PlexServerOption) => {
    await api.post('/api/admin/plex', { enabled: true, machineId: srv.machineIdentifier, serverName: srv.name })
    setServers(null)
    setMessage(`Enabled — anyone with access to “${srv.name}” can now sign in with Plex.`)
    load()
  }

  const toggle = async () => {
    if (!config) return
    await api.post('/api/admin/plex', { enabled: !config.enabled, machineId: config.machineId, serverName: config.serverName })
    load()
  }

  if (!config) return <p className="text-sm text-slate-500">Loading…</p>

  return (
    <div className="space-y-4 max-w-2xl">
      <div className="card p-6 space-y-3">
        <h3 className="font-bold">Sign in with Plex</h3>
        <p className="text-sm text-slate-400">
          Lets family members log in with the Plex accounts they already use. Only accounts with access
          to your chosen Plex server are allowed; new users are created automatically with no admin rights
          and no auto-approve.
        </p>
        {config.machineId ? (
          <div className="text-sm">
            <span className="text-slate-400">Server:</span> <b>{config.serverName || config.machineId}</b>
            <span className={`ml-3 text-xs font-semibold rounded-md px-2 py-0.5 ${config.enabled ? 'bg-emerald-500/10 text-emerald-400' : 'bg-amber-500/10 text-amber-400'}`}>
              {config.enabled ? 'enabled' : 'disabled'}
            </span>
          </div>
        ) : (
          <p className="text-sm text-slate-500">Not configured yet.</p>
        )}
        <div className="flex gap-2">
          <button className="btn-primary" onClick={connect} disabled={busy}>
            {config.machineId ? 'Reconnect / change server' : 'Connect Plex'}
          </button>
          {config.machineId && (
            <button className="btn-ghost" onClick={toggle}>
              {config.enabled ? 'Disable Plex sign-in' : 'Enable Plex sign-in'}
            </button>
          )}
        </div>
        {message && <p className="text-sm text-slate-400">{message}</p>}
        {servers && servers.length > 0 && (
          <div className="divide-y divide-white/5 border border-white/10 rounded-xl">
            {servers.map((srv) => (
              <button key={srv.machineIdentifier} onClick={() => choose(srv)} className="w-full text-left px-4 py-3 hover:bg-white/5 text-sm">
                <b>{srv.name}</b>
                {srv.owned && <span className="text-xs text-accent ml-2">owned</span>}
                <span className="block text-xs text-slate-500">{srv.machineIdentifier}</span>
              </button>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}

// ---------------- status ----------------

interface Stats {
  users: number
  cachedArtists: number
  libraryArtists: number
  requests: Record<string, number>
  jobs: Record<string, string>
  version: string
}

function Status() {
  const [stats, setStats] = useState<Stats | null>(null)
  const [msg, setMsg] = useState('')

  const load = useCallback(() => api.get<Stats>('/api/admin/stats').then(setStats), [])
  useEffect(() => {
    load()
  }, [load])

  const sync = async (job: string) => {
    setMsg(`${job} sync started…`)
    await api.post(`/api/admin/sync/${job}`)
    setTimeout(load, 3000)
  }

  if (!stats) return <p className="text-sm text-slate-500">Loading…</p>

  return (
    <div className="space-y-4">
      <div className="grid sm:grid-cols-3 gap-4">
        <StatCard label="Users" value={stats.users} />
        <StatCard label="Library artists" value={stats.libraryArtists} />
        <StatCard label="Cached artists" value={stats.cachedArtists} />
      </div>
      <div className="card p-6 space-y-2">
        <h3 className="font-bold mb-2">Requests</h3>
        <div className="flex gap-4 text-sm text-slate-400">
          {Object.entries(stats.requests).length === 0 && <span>none yet</span>}
          {Object.entries(stats.requests).map(([k, v]) => (
            <span key={k} className="capitalize">
              {k}: <b className="text-slate-200">{v}</b>
            </span>
          ))}
        </div>
      </div>
      <div className="card p-6 space-y-3">
        <h3 className="font-bold">Background jobs</h3>
        {Object.entries(stats.jobs).map(([k, v]) => (
          <div key={k} className="text-sm text-slate-400">
            <b className="text-slate-200 capitalize">{k}</b>: {v}
          </div>
        ))}
        <div className="flex gap-2 pt-2">
          <button className="btn-ghost" onClick={() => sync('trending')}>
            <RefreshCw size={14} /> Sync trending
          </button>
          <button className="btn-ghost" onClick={() => sync('library')}>
            <RefreshCw size={14} /> Sync library
          </button>
          <button className="btn-ghost" onClick={() => sync('recommendations')}>
            <RefreshCw size={14} /> Rebuild my recommendations
          </button>
        </div>
        {msg && <p className="text-xs text-slate-500">{msg}</p>}
      </div>
      <p className="text-xs text-slate-600">MusicSeer {stats.version}</p>
    </div>
  )
}

function StatCard({ label, value }: { label: string; value: number }) {
  return (
    <div className="card p-5">
      <div className="text-2xl font-bold">{value.toLocaleString()}</div>
      <div className="text-xs text-slate-500 mt-1">{label}</div>
    </div>
  )
}
