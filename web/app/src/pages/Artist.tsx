import { ArrowLeft, Check, Clock, Disc3, Music2, Pause, Play, Plus, Youtube } from 'lucide-react'
import { useEffect, useState } from 'react'
import { Link, useParams, useSearchParams } from 'react-router-dom'
import { api, ApiError } from '../api'
import { PreviewTrack, playAlbum, playUrl, subscribe } from '../audio'

interface AlbumEntry {
  mbid: string
  title: string
  type: string
  secondaryTypes?: string[]
  year?: string
  coverUrl?: string
  owned: boolean
  percent?: number
  requested: boolean
}

interface ArtistDetail {
  name: string
  mbid: string
  bio?: string
  formed?: string
  imageUrl?: string
  genres?: string[]
  listeners: number
  inLibrary: boolean
  requested: boolean
  albums: AlbumEntry[]
}

export default function Artist() {
  const { mbid } = useParams()
  const [params] = useSearchParams()
  const name = params.get('name') ?? ''
  const [detail, setDetail] = useState<ArtistDetail | null>(null)
  const [error, setError] = useState('')
  const [bioOpen, setBioOpen] = useState(false)

  useEffect(() => {
    setDetail(null)
    setError('')
    api
      .get<ArtistDetail>(`/api/artist?mbid=${encodeURIComponent(mbid ?? '')}&name=${encodeURIComponent(name)}`)
      .then(setDetail)
      .catch((e) => setError(e instanceof Error ? e.message : 'failed to load artist'))
  }, [mbid, name])

  if (error) {
    return (
      <div className="max-w-3xl space-y-4">
        <BackLink />
        <div className="card p-8 text-sm text-red-400">{error}</div>
      </div>
    )
  }
  if (!detail) {
    return (
      <div className="max-w-5xl space-y-6">
        <BackLink />
        <div className="flex gap-6">
          <div className="w-44 h-44 rounded-2xl bg-white/5 animate-pulse shrink-0" />
          <div className="flex-1 space-y-3 pt-4">
            <div className="h-7 w-1/3 bg-white/5 rounded animate-pulse" />
            <div className="h-4 w-1/2 bg-white/5 rounded animate-pulse" />
            <div className="h-4 w-2/3 bg-white/5 rounded animate-pulse" />
          </div>
        </div>
        <p className="text-xs text-slate-600">First visit fetches the discography from MusicBrainz — a second or two.</p>
      </div>
    )
  }

  const sections: [string, AlbumEntry[]][] = [
    ['Albums', detail.albums.filter((a) => a.type === 'Album' && !(a.secondaryTypes?.length))],
    ['EPs', detail.albums.filter((a) => a.type === 'EP')],
    ['Singles', detail.albums.filter((a) => a.type === 'Single')],
    ['Live & Compilations', detail.albums.filter((a) => a.type === 'Album' && (a.secondaryTypes?.length ?? 0) > 0)],
  ]

  const bio = detail.bio ?? ''
  const shortBio = bio.length > 420 ? bio.slice(0, 420).trimEnd() + '…' : bio
  const youtubeUrl = `https://www.youtube.com/results?search_query=${encodeURIComponent(detail.name)}`

  return (
    <div className="max-w-6xl space-y-8">
      <BackLink />

      <header className="flex flex-col sm:flex-row gap-6">
        <div className="w-44 h-44 rounded-2xl overflow-hidden bg-white/5 shrink-0">
          {detail.imageUrl ? (
            <img src={detail.imageUrl} alt={detail.name} className="w-full h-full object-cover" />
          ) : (
            <div className="w-full h-full flex items-center justify-center text-slate-600">
              <Music2 size={56} />
            </div>
          )}
        </div>
        <div className="min-w-0">
          <h1 className="text-3xl font-bold">{detail.name}</h1>
          <div className="text-sm text-slate-500 mt-1 space-x-2">
            {detail.formed && <span>est. {detail.formed}</span>}
            {detail.genres && detail.genres.length > 0 && <span>· {detail.genres.slice(0, 4).join(' · ')}</span>}
            {detail.listeners > 0 && <span>· {formatListeners(detail.listeners)}</span>}
          </div>
          <div className="mt-3 flex items-center gap-2 flex-wrap">
            <a href={youtubeUrl} target="_blank" rel="noreferrer" className="btn-ghost" title="Search on YouTube">
              <Youtube size={15} className="text-red-500" /> YouTube
            </a>
            {detail.inLibrary ? (
              <span className="btn bg-emerald-500/10 text-emerald-400 cursor-default">
                <Check size={15} /> In your library
              </span>
            ) : (
              <RequestButton
                label="Request entire artist"
                requested={detail.requested}
                onRequest={() => api.post('/api/requests', { artistName: detail.name, artistMbid: detail.mbid })}
              />
            )}
          </div>
          {bio && (
            <p className="text-sm text-slate-400 mt-4 max-w-3xl leading-relaxed">
              {bioOpen ? bio : shortBio}{' '}
              {bio.length > 420 && (
                <button className="text-accent hover:underline" onClick={() => setBioOpen(!bioOpen)}>
                  {bioOpen ? 'less' : 'more'}
                </button>
              )}
            </p>
          )}
        </div>
      </header>

      <TopTracks artist={detail.name} />

      {sections.map(
        ([title, entries]) =>
          entries.length > 0 && (
            <section key={title}>
              <div className="flex items-center gap-2 mb-4">
                <Disc3 size={18} className="text-accent" />
                <h2 className="text-lg font-bold">{title}</h2>
                <span className="text-xs text-slate-600">{entries.length}</span>
              </div>
              <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-6 gap-4">
                {entries.map((album) => (
                  <AlbumCard key={album.mbid} album={album} artist={detail} />
                ))}
              </div>
            </section>
          ),
      )}
      {detail.albums.length === 0 && (
        <div className="card p-8 text-sm text-slate-500">MusicBrainz lists no releases for this artist.</div>
      )}
    </div>
  )
}

function TopTracks({ artist }: { artist: string }) {
  const [tracks, setTracks] = useState<PreviewTrack[] | null>(null)
  const [playingKey, setPlayingKey] = useState<string | null>(null)

  useEffect(() => {
    api.get<{ tracks: PreviewTrack[] }>(`/api/preview?artist=${encodeURIComponent(artist)}`)
      .then((r) => setTracks(r.tracks)).catch(() => setTracks([]))
  }, [artist])
  useEffect(() => subscribe(setPlayingKey), [])

  if (tracks === null) return null
  if (tracks.length === 0)
    return (
      <p className="text-xs text-slate-600">
        No samples found on Deezer under this artist name — try the play buttons on individual albums
        below (matched by album title, more precise) or the YouTube link above.
      </p>
    )
  return (
    <section>
      <div className="flex items-center gap-2 mb-3">
        <Play size={18} className="text-accent" />
        <h2 className="text-lg font-bold">Top tracks</h2>
        <span className="text-xs text-slate-600">30-second samples</span>
      </div>
      <div className="card divide-y divide-white/5 max-w-2xl">
        {tracks.map((t, i) => {
          const key = `${artist}::${i}`
          const active = playingKey === key
          return (
            <button
              key={key}
              onClick={() => playUrl(key, t.preview)}
              className="w-full flex items-center gap-3 px-4 py-2.5 text-left hover:bg-white/5 transition-colors"
            >
              <span className={`w-7 h-7 rounded-full flex items-center justify-center shrink-0 ${active ? 'bg-accent text-white' : 'bg-white/10 text-slate-300'}`}>
                {active ? <Pause size={13} /> : <Play size={13} className="ml-0.5" />}
              </span>
              <span className="text-sm truncate">{t.title}</span>
              {active && <span className="ml-auto text-[10px] text-accent uppercase tracking-widest shrink-0">playing</span>}
            </button>
          )
        })}
      </div>
    </section>
  )
}

function AlbumCard({ album, artist }: { album: AlbumEntry; artist: ArtistDetail }) {
  const [imgOk, setImgOk] = useState(true)
  const [playing, setPlaying] = useState(false)
  const [previewBusy, setPreviewBusy] = useState(false)
  const [noPreview, setNoPreview] = useState(false)
  const previewKey = `album:${artist.name}:${album.title}`

  useEffect(() => subscribe((key) => setPlaying(key === previewKey)), [previewKey])

  const togglePreview = async () => {
    setPreviewBusy(true)
    try {
      const found = await playAlbum(artist.name, album.title)
      if (!found) setNoPreview(true)
    } catch {
      setNoPreview(true)
    }
    setPreviewBusy(false)
  }

  return (
    <div className="card p-3 flex flex-col group">
      <div className="relative aspect-square rounded-xl overflow-hidden bg-white/5 mb-3">
        {album.coverUrl && imgOk ? (
          <img
            src={album.coverUrl}
            alt={album.title}
            loading="lazy"
            className="w-full h-full object-cover"
            onError={() => setImgOk(false)}
          />
        ) : (
          <div className="w-full h-full flex items-center justify-center text-slate-600">
            <Disc3 size={36} />
          </div>
        )}
        {album.owned && (
          <span className="absolute top-2 right-2 text-[10px] font-bold bg-emerald-500/90 text-black rounded-md px-1.5 py-0.5">
            {album.percent && album.percent < 100 ? `${Math.round(album.percent)}%` : 'OWNED'}
          </span>
        )}
        {!noPreview ? (
          <button
            onClick={togglePreview}
            title="Play a 30-second sample from this album"
            className={`absolute bottom-2 right-2 w-9 h-9 rounded-full flex items-center justify-center transition-opacity shadow-lg ${
              playing ? 'bg-accent text-white opacity-100' : 'bg-black/70 text-white opacity-0 group-hover:opacity-100'
            }`}
          >
            {previewBusy ? (
              <span className="w-3 h-3 rounded-full border-2 border-white/40 border-t-white animate-spin" />
            ) : playing ? (
              <Pause size={16} />
            ) : (
              <Play size={16} className="ml-0.5" />
            )}
          </button>
        ) : (
          <span className="absolute bottom-2 right-2 text-[9px] bg-black/70 text-slate-400 rounded px-1.5 py-0.5 opacity-0 group-hover:opacity-100">
            no sample
          </span>
        )}
      </div>
      <div className="text-sm font-semibold leading-tight line-clamp-2" title={album.title}>
        {album.title}
      </div>
      <div className="text-xs text-slate-500 mb-2">{album.year || '—'}</div>
      <div className="mt-auto">
        {album.owned ? (
          <span className="btn w-full justify-center bg-emerald-500/10 text-emerald-400 cursor-default !py-1.5 text-xs">
            <Check size={13} /> In library
          </span>
        ) : (
          <RequestButton
            small
            label="Request"
            requested={album.requested}
            onRequest={() =>
              api.post('/api/requests', {
                artistName: artist.name,
                artistMbid: artist.mbid,
                albumName: album.title,
                albumMbid: album.mbid,
              })
            }
          />
        )}
      </div>
    </div>
  )
}

function RequestButton({
  label,
  requested,
  onRequest,
  small,
}: {
  label: string
  requested: boolean
  onRequest: () => Promise<unknown>
  small?: boolean
}) {
  const [state, setState] = useState<'idle' | 'busy' | 'done' | 'error'>(requested ? 'done' : 'idle')
  const [message, setMessage] = useState('')
  const size = small ? '!py-1.5 text-xs w-full justify-center' : ''

  const click = async () => {
    setState('busy')
    try {
      await onRequest()
      setState('done')
    } catch (e) {
      if (e instanceof ApiError && e.status === 409) setState('done')
      else {
        setMessage(e instanceof Error ? e.message : 'failed')
        setState('error')
        setTimeout(() => setState('idle'), 4000)
      }
    }
  }

  if (state === 'done')
    return (
      <span className={`btn bg-white/5 text-slate-400 cursor-default ${size}`}>
        <Clock size={small ? 13 : 15} /> Requested
      </span>
    )
  if (state === 'error')
    return (
      <span className={`btn bg-red-500/10 text-red-400 ${size}`} title={message}>
        {message.slice(0, 32)}
      </span>
    )
  return (
    <button onClick={click} disabled={state === 'busy'} className={`btn-primary ${size}`}>
      <Plus size={small ? 13 : 15} /> {state === 'busy' ? 'Requesting…' : label}
    </button>
  )
}

function BackLink() {
  return (
    <Link to={-1 as unknown as string} onClick={(e) => { e.preventDefault(); history.back() }} className="inline-flex items-center gap-2 text-sm text-slate-400 hover:text-white">
      <ArrowLeft size={16} /> Back
    </Link>
  )
}

function formatListeners(n: number) {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M listeners`
  if (n >= 1_000) return `${Math.round(n / 1_000)}K listeners`
  return `${n} listeners`
}
