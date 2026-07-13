import { Search as SearchIcon } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { api, ArtistItem } from '../api'
import ArtistCard from '../components/ArtistCard'

export default function Search() {
  const [query, setQuery] = useState('')
  const [results, setResults] = useState<ArtistItem[]>([])
  const [busy, setBusy] = useState(false)
  const [searched, setSearched] = useState(false)
  const timer = useRef<number>()

  useEffect(() => {
    window.clearTimeout(timer.current)
    if (query.trim().length < 2) {
      setResults([])
      setSearched(false)
      return
    }
    timer.current = window.setTimeout(async () => {
      setBusy(true)
      try {
        setResults(await api.get<ArtistItem[]>(`/api/search?q=${encodeURIComponent(query.trim())}`))
        setSearched(true)
      } catch {
        setResults([])
      } finally {
        setBusy(false)
      }
    }, 350)
    return () => window.clearTimeout(timer.current)
  }, [query])

  return (
    <div className="space-y-6 max-w-[1600px]">
      <h1 className="text-2xl font-bold">Search</h1>
      <div className="relative max-w-xl">
        <SearchIcon size={18} className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-500" />
        <input
          className="input pl-10"
          placeholder="Search for an artist…"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          autoFocus
        />
      </div>
      {busy && <p className="text-sm text-slate-500">Searching…</p>}
      {!busy && searched && results.length === 0 && (
        <p className="text-sm text-slate-500">No artists found.</p>
      )}
      <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-6 gap-4">
        {results.map((a) => (
          <ArtistCard key={a.mbid || a.name} artist={a} />
        ))}
      </div>
    </div>
  )
}
