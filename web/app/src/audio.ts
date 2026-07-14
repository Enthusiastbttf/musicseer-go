// Singleton preview player: one 30-second sample plays at a time, app-wide.
import { api } from './api'

export interface PreviewTrack {
  title: string
  preview: string
  duration: number
}

type Listener = (playingKey: string | null) => void

let audio: HTMLAudioElement | null = null
let currentKey: string | null = null
const listeners = new Set<Listener>()

function notify() {
  listeners.forEach((l) => l(currentKey))
}

export function subscribe(l: Listener): () => void {
  listeners.add(l)
  l(currentKey)
  return () => listeners.delete(l)
}

export function stop() {
  if (audio) {
    audio.pause()
    audio = null
  }
  currentKey = null
  notify()
}

/** Play a sample URL under a key (artist name or track id). Toggles off if
 *  the same key is already playing. */
export function playUrl(key: string, url: string) {
  if (currentKey === key) {
    stop()
    return
  }
  stop()
  audio = new Audio(url)
  audio.volume = 0.85
  audio.onended = () => {
    if (currentKey === key) stop()
  }
  audio.onerror = () => {
    if (currentKey === key) stop()
  }
  currentKey = key
  notify()
  audio.play().catch(() => stop())
}

/** Fetch an artist's previews and play the first one (toggle behavior). */
export async function playArtist(artist: string): Promise<boolean> {
  if (currentKey === artist) {
    stop()
    return true
  }
  const res = await api.get<{ tracks: PreviewTrack[] }>(
    `/api/preview?artist=${encodeURIComponent(artist)}`,
  )
  if (!res.tracks.length) return false
  playUrl(artist, res.tracks[0].preview)
  return true
}
