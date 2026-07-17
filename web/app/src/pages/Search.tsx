import { Disc3, Pause, Play, Search as SearchIcon } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { api, ArtistItem, TrackHit } from '../api'
import ArtistCard from '../components/ArtistCard'
import { playUrl, subscribe } from '../audio'

type Mode = 'artists' | 'songs'

// Survives unmount (visiting an artist page and coming back): the query + mode
// live in the URL, and the last result sets are kept here so back-navigation
// paints instantly instead of showing a blank page.
let cachedKey = '' // `${mode} ${query}`
let cachedArtists: ArtistItem[] = []
let cachedTracks: TrackHit[] = []

// Build an artist-page link. Track hits carry no MBID (Deezer has none), so we
// pass a "_" sentinel and let the artist page resolve the MBID from the name.
function artistLink(name: string, mbid?: string) {
  return `/artist/${mbid ? encodeURIComponent(mbid) : '_'}?name=${encodeURIComponent(name)}`
}

export default function Search() {
  const [params, setParams] = useSearchParams()
  const urlQuery = params.get('q') ?? ''
  const urlMode: Mode = params.get('mode') === 'songs' ? 'songs' : 'artists'
  const [query, setQuery] = useState(urlQuery)
  const [mode, setMode] = useState<Mode>(urlMode)
  const key = `${mode} ${query.trim()}`
  const [artists, setArtists] = useState<ArtistItem[]>(key === cachedKey ? cachedArtists : [])
  const [tracks, setTracks] = useState<TrackHit[]>(key === cachedKey ? cachedTracks : [])
  const [busy, setBusy] = useState(false)
  const [searched, setSearched] = useState(key === cachedKey && (cachedArtists.length > 0 || cachedTracks.length > 0))
  const timer = useRef<number>()

  useEffect(() => {
    window.clearTimeout(timer.current)
    const q = query.trim()
    const next: Record<string, string> = {}
    if (q) next.q = q
    if (mode === 'songs') next.mode = 'songs'
    setParams(next, { replace: true })
    if (q.length < 2) {
      setArtists([])
      setTracks([])
      setSearched(false)
      return
    }
    if (key === cachedKey && (cachedArtists.length > 0 || cachedTracks.length > 0)) {
      setArtists(cachedArtists)
      setTracks(cachedTracks)
      setSearched(true)
      return
    }
    timer.current = window.setTimeout(async () => {
      setBusy(true)
      try {
        if (mode === 'songs') {
          const found = await api.get<TrackHit[]>(`/api/search/tracks?q=${encodeURIComponent(q)}`)
          setTracks(found)
          setArtists([])
          cachedTracks = found
          cachedArtists = []
        } else {
          const found = await api.get<ArtistItem[]>(`/api/search?q=${encodeURIComponent(q)}`)
          setArtists(found)
          setTracks([])
          cachedArtists = found
          cachedTracks = []
        }
        setSearched(true)
        cachedKey = key
      } catch {
        setArtists([])
        setTracks([])
      } finally {
        setBusy(false)
      }
    }, 350)
    return () => window.clearTimeout(timer.current)
  }, [query, mode, key, setParams])

  const empty = mode === 'songs' ? tracks.length === 0 : artists.length === 0

  return (
    <div className="space-y-6 max-w-[1600px]">
      <h1 className="text-2xl font-bold">Search</h1>

      <div className="flex items-center gap-3 flex-wrap">
        <div className="relative flex-1 min-w-[16rem] max-w-xl">
          <SearchIcon size={18} className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-500" />
          <input
            className="input pl-10"
            placeholder={mode === 'songs' ? 'Search for a song…' : 'Search for an artist…'}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            autoFocus
          />
        </div>
        <div className="inline-flex rounded-lg bg-white/5 p-1 text-sm font-semibold">
          {(['artists', 'songs'] as Mode[]).map((m) => (
            <button
              key={m}
              onClick={() => setMode(m)}
              className={`px-4 py-1.5 rounded-md capitalize transition-colors ${
                mode === m ? 'bg-accent text-white' : 'text-slate-300 hover:text-white'
              }`}
            >
              {m}
            </button>
          ))}
        </div>
      </div>

      {busy && <p className="text-sm text-slate-500">Searching…</p>}
      {!busy && searched && empty && (
        <p className="text-sm text-slate-500">{mode === 'songs' ? 'No songs found.' : 'No artists found.'}</p>
      )}

      {mode === 'artists' ? (
        <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-6 gap-4">
          {artists.map((a) => (
            <ArtistCard key={a.mbid || a.name} artist={a} />
          ))}
        </div>
      ) : (
        <div className="flex flex-col gap-1 max-w-3xl">
          {tracks.map((t, i) => (
            <TrackRow key={`${t.artist}-${t.track}-${i}`} track={t} rowKey={`trk:${i}:${t.preview}`} />
          ))}
        </div>
      )}
    </div>
  )
}

function TrackRow({ track, rowKey }: { track: TrackHit; rowKey: string }) {
  const [playing, setPlaying] = useState(false)
  useEffect(() => subscribe((k) => setPlaying(k === rowKey)), [rowKey])

  return (
    <div className="flex items-center gap-3 p-2 rounded-lg hover:bg-white/5 group">
      <div className="relative w-12 h-12 shrink-0 rounded-md overflow-hidden bg-white/5">
        {track.coverUrl ? (
          <img src={track.coverUrl} alt="" className="w-full h-full object-cover" />
        ) : (
          <div className="w-full h-full grid place-items-center text-slate-600">
            <Disc3 size={20} />
          </div>
        )}
        {track.preview && (
          <button
            onClick={() => playUrl(rowKey, track.preview!)}
            className="absolute inset-0 grid place-items-center bg-black/45 opacity-0 group-hover:opacity-100 data-[on=true]:opacity-100 transition-opacity"
            data-on={playing}
            title={playing ? 'Pause' : 'Play 30-second preview'}
          >
            {playing ? <Pause size={18} className="text-white" /> : <Play size={18} className="text-white" />}
          </button>
        )}
      </div>
      <div className="min-w-0 flex-1">
        <div className="font-semibold text-sm truncate" title={track.track}>
          {track.track}
        </div>
        <div className="text-xs text-slate-400 truncate">
          <Link to={artistLink(track.artist)} className="hover:text-accent hover:underline">
            {track.artist}
          </Link>
          {track.album ? <span className="text-slate-500"> · {track.album}</span> : null}
        </div>
      </div>
      {track.inLibrary ? (
        <span className="text-[11px] px-2 py-0.5 rounded-full bg-emerald-500/15 text-emerald-400 shrink-0">
          In library
        </span>
      ) : track.requested ? (
        <span className="text-[11px] px-2 py-0.5 rounded-full bg-amber-500/15 text-amber-400 shrink-0">Requested</span>
      ) : null}
      <Link
        to={artistLink(track.artist)}
        className="text-xs text-slate-400 hover:text-accent shrink-0 px-2"
        title="Go to artist"
      >
        View artist →
      </Link>
    </div>
  )
}
