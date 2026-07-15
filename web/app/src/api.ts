// Tiny fetch wrapper: cookie sessions, JSON in/out, typed errors.

export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    credentials: 'same-origin',
    headers: body !== undefined ? { 'Content-Type': 'application/json' } : undefined,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  })
  const text = await res.text()
  const data = text ? JSON.parse(text) : null
  if (!res.ok) throw new ApiError(res.status, data?.error ?? res.statusText)
  return data as T
}

export const api = {
  get: <T>(path: string) => request<T>('GET', path),
  post: <T>(path: string, body?: unknown) => request<T>('POST', path, body),
  put: <T>(path: string, body?: unknown) => request<T>('PUT', path, body),
  del: <T>(path: string) => request<T>('DELETE', path),
}

// ---- shared types ----

export interface User {
  id: number
  username: string
  email?: string
  role: 'admin' | 'user'
  canAutoApprove: boolean
  lastfmUser?: string
}

export interface ArtistItem {
  rank?: number
  name: string
  mbid?: string
  disambiguation?: string
  imageUrl?: string
  genres?: string[]
  listeners?: number
  reason?: string
  inLibrary?: boolean
  requested?: boolean
}

export interface RecsResponse {
  items: ArtistItem[]
  computedAt?: string
  computing?: boolean
}

export interface RequestItem {
  id: number
  userId: number
  username: string
  artistName: string
  artistMbid?: string
  albumName?: string
  albumMbid?: string
  status: 'pending' | 'approved' | 'rejected' | 'sent' | 'failed'
  notes?: string
  error?: string
  createdAt: string
  updatedAt: string
}

export interface Instance {
  id: number
  name: string
  type: 'navidrome' | 'lidarr'
  baseUrl: string
  username?: string
  isActive: boolean
  isAuthSource: boolean
  qualityProfileId?: number
  metadataProfileId?: number
  rootFolder?: string
}
