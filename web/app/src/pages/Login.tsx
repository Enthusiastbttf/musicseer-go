import { Gem } from 'lucide-react'
import { FormEvent, useState } from 'react'
import { api, User } from '../api'
import { useAuth } from '../App'

export default function Login() {
  const { setUser } = useAuth()
  const [login, setLogin] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

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
        <p className="text-xs text-slate-500 text-center">
          Navidrome users can sign in with their Navidrome credentials.
        </p>
      </form>
    </div>
  )
}
