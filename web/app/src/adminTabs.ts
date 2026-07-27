// The Admin sections live in the URL (/admin/:tab) rather than in component
// state, so they can be deep-linked, bookmarked and reached from the sidebar,
// and so browser Back moves between them. Shared by the Admin page and the
// sidebar sub-nav so the two lists can never drift apart.
export const ADMIN_TABS = ['instances', 'users', 'connections', 'status'] as const

export type AdminTab = (typeof ADMIN_TABS)[number]

export const DEFAULT_ADMIN_TAB: AdminTab = 'instances'

export function isAdminTab(v: string | undefined): v is AdminTab {
  return !!v && (ADMIN_TABS as readonly string[]).includes(v)
}
