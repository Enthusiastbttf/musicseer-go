import { createContext, useContext, useEffect, useState } from 'react'
import { Navigate, Route, Routes, useNavigate } from 'react-router-dom'
import { api, User } from './api'
import Layout from './components/Layout'
import Admin from './pages/Admin'
import Artist from './pages/Artist'
import Home from './pages/Home'
import Login from './pages/Login'
import Requests from './pages/Requests'
import Search from './pages/Search'
import Setup from './pages/Setup'

interface AuthState {
  user: User | null
  version: string
  setUser: (u: User | null) => void
  logout: () => Promise<void>
}

const AuthContext = createContext<AuthState>(null!)
export const useAuth = () => useContext(AuthContext)

interface Status {
  setupComplete: boolean
  user?: User
  version?: string
}

export default function App() {
  const [loading, setLoading] = useState(true)
  const [setupComplete, setSetupComplete] = useState(true)
  const [user, setUser] = useState<User | null>(null)
  const [version, setVersion] = useState('')
  const navigate = useNavigate()

  useEffect(() => {
    api
      .get<Status>('/api/status')
      .then((s) => {
        setSetupComplete(s.setupComplete)
        setUser(s.user ?? null)
        setVersion(s.version ?? '')
      })
      .finally(() => setLoading(false))
  }, [])

  const logout = async () => {
    try {
      await api.post('/api/auth/logout')
    } finally {
      setUser(null)
      navigate('/login')
    }
  }

  if (loading) {
    return (
      <div className="h-screen flex items-center justify-center text-slate-500">
        Loading…
      </div>
    )
  }

  return (
    <AuthContext.Provider value={{ user, version, setUser, logout }}>
      <Routes>
        {!setupComplete && (
          <Route path="*" element={<Setup onDone={() => setSetupComplete(true)} />} />
        )}
        {setupComplete && !user && (
          <>
            <Route path="/login" element={<Login />} />
            <Route path="*" element={<Navigate to="/login" replace />} />
          </>
        )}
        {setupComplete && user && (
          <Route element={<Layout />}>
            <Route path="/" element={<Home />} />
            <Route path="/search" element={<Search />} />
            <Route path="/artist/:mbid" element={<Artist />} />
            <Route path="/requests" element={<Requests />} />
            {user.role === 'admin' && <Route path="/admin" element={<Admin />} />}
            <Route path="*" element={<Navigate to="/" replace />} />
          </Route>
        )}
      </Routes>
    </AuthContext.Provider>
  )
}
