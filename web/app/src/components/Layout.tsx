import { Gem, Home, ListMusic, LogOut, Search, Settings, User as UserIcon } from 'lucide-react'
import { NavLink, Outlet } from 'react-router-dom'
import { ADMIN_TABS } from '../adminTabs'
import { useAuth } from '../App'

const linkClass = ({ isActive }: { isActive: boolean }) =>
  `flex items-center gap-3 px-4 py-2.5 rounded-xl text-sm font-medium transition-colors ${
    isActive ? 'bg-accent/15 text-accent' : 'text-slate-400 hover:text-white hover:bg-white/5'
  }`

// Sub-items are deliberately quieter than top-level nav — no pill background,
// smaller type — so the four admin sections read as children of Admin rather
// than as four more primary destinations.
const subLinkClass = ({ isActive }: { isActive: boolean }) =>
  `block rounded-lg px-3 py-1.5 text-[13px] capitalize transition-colors ${
    isActive ? 'text-accent font-semibold' : 'text-slate-500 hover:text-slate-200 hover:bg-white/5'
  }`

export default function Layout() {
  const { user, version, logout } = useAuth()
  return (
    <div className="flex min-h-screen">
      <aside className="w-60 shrink-0 border-r border-white/5 bg-black/40 flex flex-col p-4 sticky top-0 h-screen">
        <div className="flex items-center gap-2.5 px-2 py-4">
          <div className="w-9 h-9 rounded-xl bg-accent flex items-center justify-center shadow-lg shadow-accent/20">
            <Gem size={18} className="text-accent-ink" />
          </div>
          <div className="flex flex-col leading-none">
            <span className="text-lg font-bold tracking-tight">MusicSeer</span>
            <span className="text-[10px] font-semibold tracking-widest text-accent uppercase mt-0.5">
              Enhanced{version && <span className="text-slate-500 font-normal tracking-normal normal-case"> · v{version}</span>}
            </span>
          </div>
        </div>
        <nav className="mt-4 space-y-1">
          <NavLink to="/" end className={linkClass}>
            <Home size={18} /> Discover
          </NavLink>
          <NavLink to="/search" className={linkClass}>
            <Search size={18} /> Search
          </NavLink>
          <NavLink to="/requests" className={linkClass}>
            <ListMusic size={18} /> Requests
          </NavLink>
          {user?.role === 'admin' && (
            <div>
              <NavLink to="/admin" className={linkClass}>
                <Settings size={18} /> Admin
              </NavLink>
              {/* Always shown rather than expand-on-active: the point is to
                  reach any admin section in one click from anywhere. */}
              <div className="mt-1 ml-6 space-y-0.5 border-l border-white/10 pl-2">
                {ADMIN_TABS.map((t) => (
                  <NavLink key={t} to={`/admin/${t}`} className={subLinkClass}>
                    {t}
                  </NavLink>
                ))}
              </div>
            </div>
          )}
        </nav>
        <div className="mt-auto space-y-2">
          <div className="flex items-center gap-3 px-3 py-3 rounded-xl bg-white/5">
            <div className="w-8 h-8 rounded-full bg-accent/30 flex items-center justify-center">
              <UserIcon size={16} />
            </div>
            <div className="min-w-0">
              <div className="text-sm font-semibold truncate">{user?.username}</div>
              <div className="text-xs text-slate-500 capitalize">{user?.role}</div>
            </div>
          </div>
          <button onClick={logout} className="btn-ghost w-full justify-center text-slate-400 hover:text-red-400">
            <LogOut size={16} /> Sign out
          </button>
        </div>
      </aside>
      <main className="flex-1 min-w-0 p-8">
        <Outlet />
      </main>
    </div>
  )
}
