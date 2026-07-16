import { Globe2, RefreshCw, Sparkles, TrendingUp } from 'lucide-react'
import { ReactNode, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, ArtistItem, RecsResponse } from '../api'
import ArtistCard from '../components/ArtistCard'

function Section({
  icon,
  title,
  subtitle,
  items,
  loading,
  computing,
}: {
  icon: ReactNode
  title: string
  subtitle?: string
  items: ArtistItem[]
  loading: boolean
  computing?: boolean
}) {
  return (
    <section>
      <div className="flex items-center gap-3 mb-4">
        <span className="text-accent">{icon}</span>
        <h2 className="text-xl font-bold">{title}</h2>
        {subtitle && <span className="text-xs text-slate-500">{subtitle}</span>}
      </div>
      {loading ? (
        <SkeletonRow />
      ) : computing ? (
        <div className="card p-6 text-sm text-slate-400 flex items-center gap-3">
          <RefreshCw size={16} className="animate-spin" />
          Building your recommendations from your library — check back in a minute.
        </div>
      ) : items.length === 0 ? (
        <div className="card p-6 text-sm text-slate-500">Nothing here yet.</div>
      ) : (
        <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-6 gap-4">
          {items.map((a) => (
            <ArtistCard key={a.mbid || a.name} artist={a} />
          ))}
        </div>
      )}
    </section>
  )
}

function SkeletonRow() {
  return (
    <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-6 gap-4">
      {Array.from({ length: 6 }).map((_, i) => (
        <div key={i} className="card p-3">
          <div className="aspect-square rounded-xl bg-white/5 animate-pulse mb-3" />
          <div className="h-3.5 bg-white/5 rounded animate-pulse mb-2" />
          <div className="h-3 w-2/3 bg-white/5 rounded animate-pulse" />
        </div>
      ))}
    </div>
  )
}


interface GenresResponse {
  explore: string[]
  browse: string[]
}

// Deterministic gradient per genre so tiles are stable between renders.
const gradients = [
  'from-orange-600 to-amber-800', 'from-emerald-600 to-teal-800',
  'from-pink-600 to-rose-800', 'from-violet-600 to-indigo-800',
  'from-sky-600 to-blue-800', 'from-red-600 to-orange-800',
  'from-fuchsia-600 to-purple-800', 'from-lime-600 to-green-800',
]
function gradientFor(genre: string) {
  let h = 0
  for (const c of genre) h = (h * 31 + c.charCodeAt(0)) >>> 0
  return gradients[h % gradients.length]
}

function GenresSection() {
  const [genres, setGenres] = useState<GenresResponse | null>(null)
  useEffect(() => {
    api.get<GenresResponse>('/api/discovery/genres').then(setGenres).catch(() => {})
  }, [])
  if (!genres) return null
  return (
    <section>
      <div className="flex items-center gap-3 mb-4">
        <span className="text-accent"><Globe2 size={20} /></span>
        <h2 className="text-xl font-bold">Genres to Explore</h2>
        {genres.explore.length > 0 && <span className="text-xs text-slate-500">from your library</span>}
      </div>
      {genres.explore.length > 0 && (
        <div className="flex flex-wrap gap-2 mb-6">
          {genres.explore.map((g) => (
            <Link
              key={g}
              to={`/genre/${encodeURIComponent(g)}`}
              className={`px-4 py-1.5 rounded-full text-sm font-semibold capitalize text-white bg-gradient-to-br ${gradientFor(g)} hover:opacity-90 transition-opacity`}
            >
              {g}
            </Link>
          ))}
        </div>
      )}
      <h3 className="text-sm font-semibold text-slate-400 mb-3">Browse by genre</h3>
      <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5 gap-4">
        {genres.browse.map((g) => (
          <Link
            key={g}
            to={`/genre/${encodeURIComponent(g)}`}
            className={`h-24 rounded-2xl bg-gradient-to-br ${gradientFor(g)} p-4 flex items-end font-bold capitalize text-white shadow-lg hover:scale-[1.02] transition-transform`}
          >
            {g}
          </Link>
        ))}
      </div>
    </section>
  )
}

export default function Home() {
  const [trending, setTrending] = useState<ArtistItem[]>([])
  const [recs, setRecs] = useState<RecsResponse | null>(null)
  const [loadingTrending, setLoadingTrending] = useState(true)
  const [loadingRecs, setLoadingRecs] = useState(true)

  // Both sections come out of SQLite server-side, so they load in
  // milliseconds — no spinner marathon like the old app.
  useEffect(() => {
    api.get<ArtistItem[]>('/api/discovery/trending?limit=18')
      .then(setTrending).catch(() => {}).finally(() => setLoadingTrending(false))
    api.get<RecsResponse>('/api/discovery/recommendations?limit=18')
      .then(setRecs).catch(() => {}).finally(() => setLoadingRecs(false))
  }, [])

  // If recommendations are still computing, poll gently until they arrive.
  useEffect(() => {
    if (!recs?.computing) return
    const t = setInterval(async () => {
      const r = await api.get<RecsResponse>('/api/discovery/recommendations?limit=18').catch(() => null)
      if (r) setRecs(r)
      if (r && !r.computing) clearInterval(t)
    }, 8000)
    return () => clearInterval(t)
  }, [recs?.computing])

  return (
    <div className="space-y-10 max-w-[1600px]">
      <h1 className="text-2xl font-bold">Discover</h1>
      <Section icon={<TrendingUp size={20} />} title="Trending Now" items={trending} loading={loadingTrending} />
      <Section
        icon={<Sparkles size={20} />}
        title="Similar to Your Library"
        items={recs?.items ?? []}
        loading={loadingRecs}
        computing={recs?.computing}
      />
      <GenresSection />
    </div>
  )
}
