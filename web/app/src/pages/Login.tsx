import { Gem } from 'lucide-react'
import { FormEvent, useRef, useState } from 'react'
import { api, User } from '../api'
import { useAuth } from '../App'

export default function Login() {
  const { setUser, plexLogin } = useAuth()
  const [login, setLogin] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [plexBusy, setPlexBusy] = useState(false)
  const pollTimer = useRef<number>()

  const signInWithPlex = async () => {
    setError('')
    setPlexBusy(true)
    try {
      const start = await api.post<{ pinId: number; authUrl: string }>('/api/auth/plex/start')
      window.open(start.authUrl, '_blank', 'noopener,width=600,height=700')
      const poll = async () => {
        try {
          const res = await api.post<{ pending?: boolean; user?: User }>('/api/auth/plex/poll', {
            pinId: start.pinId,
          })
          if (res.user) {
            setUser(res.user)
            return
          }
          pollTimer.current = window.setTimeout(poll, 2000)
        } catch (e) {
          setError(e instanceof Error ? e.message : 'Plex sign-in failed')
          setPlexBusy(false)
        }
      }
      poll()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Plex sign-in failed')
      setPlexBusy(false)
    }
  }

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError('')
    try {
      const res = await api.post<{ user: User }>('/api/auth/login', { login, password })
      setUser(res.user)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'login failed')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="min-h-screen flex items-center justify-center p-6">
      <form onSubmit={submit} className="card p-8 w-full max-w-sm space-y-4">
        <div className="flex items-center gap-3 mb-2">
          <div className="w-10 h-10 rounded-xl bg-accent flex items-center justify-center">
            <Gem size={20} className="text-white" />
          </div>
          <div>
            <h1 className="text-xl font-bold leading-none">MusicSeer</h1>
            <span className="text-[10px] font-semibold tracking-widest text-accent uppercase">Enhanced</span>
          </div>
        </div>
        <input className="input" placeholder="Username or email" value={login} onChange={(e) => setLogin(e.target.value)} autoFocus required />
        <input className="input" type="password" placeholder="Password" value={password} onChange={(e) => setPassword(e.target.value)} required />
        {error && <p className="text-sm text-red-400">{error}</p>}
        <button className="btn-primary w-full justify-center" disabled={busy}>
          {busy ? 'Signing in…' : 'Sign in'}
        </button>
        {plexLogin && (
          <>
            <div className="flex items-center gap-3 text-xs text-slate-600">
              <span className="flex-1 h-px bg-white/10" />or<span className="flex-1 h-px bg-white/10" />
            </div>
            <button
              type="button"
              onClick={signInWithPlex}
              disabled={plexBusy}
              className="btn w-full justify-center bg-[#e5a00d] text-black font-semibold hover:bg-[#f5b025]"
            >
              {plexBusy ? 'Waiting for Plex approval…' : 'Sign in with Plex'}
            </button>
          </>
        )}
        <p className="text-xs text-slate-500 text-center">
          Navidrome users can sign in with their Navidrome credentials.
        </p>
      </form>
    </div>
  )
}
