import { Gem, RefreshCw, Sparkles, TrendingUp } from 'lucide-react'
import { ReactNode, useEffect, useState } from 'react'
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


export default function Home() {
  const [trending, setTrending] = useState<ArtistItem[]>([])
  const [recs, setRecs] = useState<RecsResponse | null>(null)
  const [gems, setGems] = useState<RecsResponse | null>(null)
  const [loadingTrending, setLoadingTrending] = useState(true)
  const [loadingRecs, setLoadingRecs] = useState(true)
  const [loadingGems, setLoadingGems] = useState(true)

  // All three sections come out of SQLite server-side, so they load in
  // milliseconds — no spinner marathon like the old app.
  useEffect(() => {
    api.get<ArtistItem[]>('/api/discovery/trending?limit=18')
      .then(setTrending).catch(() => {}).finally(() => setLoadingTrending(false))
    api.get<RecsResponse>('/api/discovery/recommendations?limit=18')
      .then(setRecs).catch(() => {}).finally(() => setLoadingRecs(false))
    api.get<RecsResponse>('/api/discovery/hidden-gems?limit=18')
      .then(setGems).catch(() => {}).finally(() => setLoadingGems(false))
  }, [])

  // If recommendations are still computing, poll gently until they arrive.
  useEffect(() => {
    if (!recs?.computing && !gems?.computing) return
    const t = setInterval(async () => {
      const [r, g] = await Promise.all([
        api.get<RecsResponse>('/api/discovery/recommendations?limit=18').catch(() => null),
        api.get<RecsResponse>('/api/discovery/hidden-gems?limit=18').catch(() => null),
      ])
      if (r) setRecs(r)
      if (g) setGems(g)
      if (r && !r.computing && g && !g.computing) clearInterval(t)
    }, 8000)
    return () => clearInterval(t)
  }, [recs?.computing, gems?.computing])

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
      <Section
        icon={<Gem size={20} />}
        title="Hidden Gems"
        subtitle="loved by fans, under 500K listeners"
        items={gems?.items ?? []}
        loading={loadingGems}
        computing={gems?.computing}
      />
    </div>
  )
}
