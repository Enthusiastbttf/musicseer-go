import { Gem } from 'lucide-react'
import { FormEvent, useState } from 'react'
import { api, User } from '../api'
import { useAuth } from '../App'

export default function Setup({ onDone }: { onDone: () => void }) {
  const { setUser } = useAuth()
  const [username, setUsername] = useState('')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError('')
    try {
      const res = await api.post<{ user: User }>('/api/auth/setup', { username, email, password })
      setUser(res.user)
      onDone()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'setup failed')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="min-h-screen flex items-center justify-center p-6">
      <form onSubmit={submit} className="card p-8 w-full max-w-md space-y-4">
        <div className="flex items-center gap-3 mb-2">
          <div className="w-10 h-10 rounded-xl bg-accent flex items-center justify-center">
            <Gem size={20} className="text-white" />
          </div>
          <div>
            <h1 className="text-xl font-bold">Welcome to MusicSeer Enhanced</h1>
            <p className="text-sm text-slate-500">Create the admin account to get started.</p>
          </div>
        </div>
        <input className="input" placeholder="Username" value={username} onChange={(e) => setUsername(e.target.value)} required />
        <input className="input" type="email" placeholder="Email (optional)" value={email} onChange={(e) => setEmail(e.target.value)} />
        <input className="input" type="password" placeholder="Password (min 8 characters)" value={password} onChange={(e) => setPassword(e.target.value)} required minLength={8} />
        {error && <p className="text-sm text-red-400">{error}</p>}
        <button className="btn-primary w-full justify-center" disabled={busy}>
          {busy ? 'Creating…' : 'Create admin account'}
        </button>
      </form>
    </div>
  )
}
