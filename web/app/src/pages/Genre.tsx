import { ArrowLeft, Tag } from 'lucide-react'
import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { api, ArtistItem } from '../api'
import ArtistCard from '../components/ArtistCard'

export default function Genre() {
  const { name } = useParams()
  const [items, setItems] = useState<ArtistItem[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  useEffect(() => {
    setLoading(true)
    setError('')
    api
      .get<ArtistItem[]>(`/api/discovery/genre?name=${encodeURIComponent(name ?? '')}`)
      .then(setItems)
      .catch((e) => setError(e instanceof Error ? e.message : 'failed to load genre'))
      .finally(() => setLoading(false))
  }, [name])

  return (
    <div className="max-w-6xl space-y-6">
      <Link to="/" className="inline-flex items-center gap-2 text-sm text-slate-400 hover:text-white">
        <ArrowLeft size={16} /> Discover
      </Link>
      <div className="flex items-center gap-3">
        <Tag size={22} className="text-accent" />
        <h1 className="text-2xl font-bold capitalize">{name}</h1>
      </div>
      {loading && (
        <p className="text-sm text-slate-500">
          Loading artists — first visit to a genre asks MusicBrainz, then it's cached…
        </p>
      )}
      {error && <div className="card p-6 text-sm text-red-400">{error}</div>}
      {!loading && !error && items.length === 0 && (
        <div className="card p-6 text-sm text-slate-500">MusicBrainz has no artists tagged “{name}”.</div>
      )}
      <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-6 gap-4">
        {items.map((a) => (
          <ArtistCard key={a.mbid || a.name} artist={a} />
        ))}
      </div>
    </div>
  )
}
