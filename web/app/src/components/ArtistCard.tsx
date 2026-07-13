import { Check, Clock, Music2, Plus } from 'lucide-react'
import { useState } from 'react'
import { api, ApiError, ArtistItem } from '../api'

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

  return (
    <div className="card p-3 group flex flex-col">
      <div className="relative aspect-square rounded-xl overflow-hidden bg-white/5 mb-3">
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
      </div>
      <div className="font-semibold text-sm truncate" title={artist.name}>
        {artist.name}
      </div>
      <div className="text-xs text-slate-500 truncate h-4">
        {artist.genres?.slice(0, 3).join(' · ') || formatListeners(artist.listeners) || artist.reason || ''}
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
