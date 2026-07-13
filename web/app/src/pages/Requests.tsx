import { Check, RefreshCw, Trash2, X } from 'lucide-react'
import { useCallback, useEffect, useState } from 'react'
import { api, RequestItem } from '../api'
import { useAuth } from '../App'

const statusStyle: Record<string, string> = {
  pending: 'bg-amber-500/10 text-amber-400',
  approved: 'bg-sky-500/10 text-sky-400',
  sent: 'bg-emerald-500/10 text-emerald-400',
  rejected: 'bg-slate-500/10 text-slate-400',
  failed: 'bg-red-500/10 text-red-400',
}

export default function Requests() {
  const { user } = useAuth()
  const isAdmin = user?.role === 'admin'
  const [showAll, setShowAll] = useState(isAdmin)
  const [items, setItems] = useState<RequestItem[]>([])
  const [loading, setLoading] = useState(true)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      setItems(await api.get<RequestItem[]>(`/api/requests${showAll ? '?all=1' : ''}`))
    } finally {
      setLoading(false)
    }
  }, [showAll])

  useEffect(() => {
    load()
  }, [load])

  const act = async (id: number, action: 'approve' | 'reject' | 'retry' | 'delete') => {
    if (action === 'delete') await api.del(`/api/requests/${id}`)
    else await api.post(`/api/requests/${id}/${action}`)
    load()
  }

  return (
    <div className="space-y-6 max-w-5xl">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Requests</h1>
        <div className="flex gap-2">
          {isAdmin && (
            <button className="btn-ghost" onClick={() => setShowAll(!showAll)}>
              {showAll ? 'Show mine' : 'Show all users'}
            </button>
          )}
          <button className="btn-ghost" onClick={load}>
            <RefreshCw size={15} /> Refresh
          </button>
        </div>
      </div>

      {loading ? (
        <p className="text-sm text-slate-500">Loading…</p>
      ) : items.length === 0 ? (
        <div className="card p-8 text-center text-slate-500 text-sm">
          No requests yet — find something on the Discover or Search page.
        </div>
      ) : (
        <div className="card divide-y divide-white/5">
          {items.map((r) => (
            <div key={r.id} className="flex items-center gap-4 p-4">
              <div className="min-w-0 flex-1">
                <div className="font-semibold text-sm truncate">{r.artistName}</div>
                <div className="text-xs text-slate-500">
                  {showAll && <span className="mr-2">by {r.username}</span>}
                  {new Date(r.createdAt).toLocaleString()}
                  {r.error && <span className="text-red-400 ml-2" title={r.error}>· {r.error.slice(0, 80)}</span>}
                  {r.notes && <span className="ml-2">· {r.notes}</span>}
                </div>
              </div>
              <span className={`text-xs font-semibold rounded-md px-2 py-1 capitalize ${statusStyle[r.status]}`}>
                {r.status}
              </span>
              {isAdmin && (
                <div className="flex gap-1.5">
                  {r.status === 'pending' && (
                    <>
                      <button className="btn-ghost !px-2.5" title="Approve & send to Lidarr" onClick={() => act(r.id, 'approve')}>
                        <Check size={15} className="text-emerald-400" />
                      </button>
                      <button className="btn-ghost !px-2.5" title="Reject" onClick={() => act(r.id, 'reject')}>
                        <X size={15} className="text-red-400" />
                      </button>
                    </>
                  )}
                  {r.status === 'failed' && (
                    <button className="btn-ghost !px-2.5" title="Retry" onClick={() => act(r.id, 'retry')}>
                      <RefreshCw size={15} className="text-sky-400" />
                    </button>
                  )}
                  <button className="btn-ghost !px-2.5" title="Delete" onClick={() => act(r.id, 'delete')}>
                    <Trash2 size={15} className="text-slate-500" />
                  </button>
                </div>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
