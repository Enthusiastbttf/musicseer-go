import { Search as SearchIcon } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { api, ArtistItem } from '../api'
import ArtistCard from '../components/ArtistCard'

// Survives unmount (e.g. visiting an artist page and coming back): the query
// lives in the URL, and the last result set is kept here so back-navigation
// paints instantly instead of showing a blank page.
let cachedQuery = ''
let cachedResults: ArtistItem[] = []

export default function Search() {
  const [params, setParams] = useSearchParams()
  const urlQuery = params.get('q') ?? ''
  const [query, setQuery] = useState(urlQuery)
  const [results, setResults] = useState<ArtistItem[]>(urlQuery === cachedQuery ? cachedResults : [])
  const [busy, setBusy] = useState(false)
  const [searched, setSearched] = useState(urlQuery === cachedQuery && cachedResults.length > 0)
  const timer = useRef<number>()

  useEffect(() => {
    window.clearTimeout(timer.current)
    const q = query.trim()
    setParams(q ? { q } : {}, { replace: true })
    if (q.length < 2) {
      setResults([])
      setSearched(false)
      return
    }
    if (q === cachedQuery && cachedResults.length > 0) {
      setResults(cachedResults)
      setSearched(true)
      return
    }
    timer.current = window.setTimeout(async () => {
      setBusy(true)
      try {
        const found = await api.get<ArtistItem[]>(`/api/search?q=${encodeURIComponent(q)}`)
        setResults(found)
        setSearched(true)
        cachedQuery = q
        cachedResults = found
      } catch {
        setResults([])
      } finally {
        setBusy(false)
      }
    }, 350)
    return () => window.clearTimeout(timer.current)
  }, [query, setParams])

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
