import { Check, Clock, Music2, Pause, Play, Plus } from 'lucide-react'
import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, ApiError, ArtistItem } from '../api'
import { playArtist, subscribe } from '../audio'

function formatListeners(n?: number) {
  if (!n) return null
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M listeners`
  if (n >= 1_000) return `${Math.round(n / 1_000)}K listeners`
  return `${n} listeners`
}

export default function ArtistCard({ artist }: { artist: ArtistItem }) {
  const [state, setState] = useState<'idle' | 'busy' | 'requested' | 'error'>(
    artist.requested ? 'requested' : 'idle',
  )
  const [message, setMessage] = useState('')
  const [playing, setPlaying] = useState(false)
  const [previewBusy, setPreviewBusy] = useState(false)
  const [noPreview, setNoPreview] = useState(false)

  useEffect(() => subscribe((key) => setPlaying(key === artist.name)), [artist.name])

  const togglePreview = async (e: React.MouseEvent) => {
    e.preventDefault()
    e.stopPropagation()
    setPreviewBusy(true)
    try {
      const found = await playArtist(artist.name)
      if (!found) setNoPreview(true)
    } catch {
      setNoPreview(true)
    }
    setPreviewBusy(false)
  }

  const request = async () => {
    setState('busy')
    try {
      await api.post('/api/requests', { artistName: artist.name, artistMbid: artist.mbid ?? '' })
      setState('requested')
    } catch (e) {
      if (e instanceof ApiError && e.status === 409) {
        setState('requested')
      } else {
        setMessage(e instanceof Error ? e.message : 'request failed')
        setState('error')
        setTimeout(() => setState('idle'), 4000)
      }
    }
  }

  const detailUrl = artist.mbid
    ? `/artist/${encodeURIComponent(artist.mbid)}?name=${encodeURIComponent(artist.name)}`
    : null

  const artwork = (
    <>
        {artist.imageUrl ? (
          <img
            src={artist.imageUrl}
            alt={artist.name}
            loading="lazy"
            className="w-full h-full object-cover group-hover:scale-105 transition-transform duration-300"
          />
        ) : (
          <div className="w-full h-full flex items-center justify-center text-slate-600">
            <Music2 size={40} />
          </div>
        )}
        {artist.rank !== undefined && (
          <span className="absolute top-2 left-2 text-xs font-bold bg-black/70 rounded-md px-1.5 py-0.5">
            #{artist.rank}
          </span>
        )}
    </>
  )

  return (
    <div className="card p-3 group flex flex-col">
      <div className="relative aspect-square rounded-xl overflow-hidden bg-white/5 mb-3">
        {detailUrl ? <Link to={detailUrl}>{artwork}</Link> : artwork}
        {noPreview ? (
          <span className="absolute bottom-2 right-2 text-[9px] bg-black/70 text-slate-400 rounded px-1.5 py-0.5 opacity-0 group-hover:opacity-100">
            no sample
          </span>
        ) : (
          <button
            onClick={togglePreview}
            title="Play a 30-second sample (matched by artist name — for identically-named artists, use the album play buttons on the artist page instead)"
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
        )}
      </div>
      {detailUrl ? (
        <Link to={detailUrl} className="font-semibold text-sm truncate hover:text-accent hover:underline" title={artist.name}>
          {artist.name}
        </Link>
      ) : (
        <div className="font-semibold text-sm truncate" title={artist.name}>
          {artist.name}
        </div>
      )}
      <div className="text-xs text-slate-500 truncate h-4" title={artist.disambiguation}>
        {artist.disambiguation || artist.genres?.slice(0, 3).join(' · ') || formatListeners(artist.listeners) || artist.reason || ''}
      </div>
      <div className="mt-3">
        {artist.inLibrary ? (
          <span className="btn w-full justify-center bg-emerald-500/10 text-emerald-400 cursor-default">
            <Check size={15} /> In library
          </span>
        ) : state === 'requested' ? (
          <span className="btn w-full justify-center bg-white/5 text-slate-400 cursor-default">
            <Clock size={15} /> Requested
          </span>
        ) : state === 'error' ? (
          <span className="btn w-full justify-center bg-red-500/10 text-red-400 text-xs" title={message}>
            {message.slice(0, 40)}
          </span>
        ) : (
          <button onClick={request} disabled={state === 'busy'} className="btn-primary w-full justify-center">
            <Plus size={15} /> {state === 'busy' ? 'Requesting…' : 'Request'}
          </button>
        )}
      </div>
    </div>
  )
}
