export type Json = Record<string, any>

const token = document.querySelector<HTMLMetaElement>('meta[name="mar-owner-token"]')?.content || ''

export async function api<T = any>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers || {})
  headers.set('Content-Type', 'application/json')
  if (token) headers.set('X-MAR-Owner-Token', token)
  const response = await fetch(path, { ...init, headers, cache: 'no-store' })
  let body: any = {}
  try { body = await response.json() } catch { /* non-json response */ }
  if (!response.ok) throw new Error(body?.error || `${response.status} ${response.statusText}`)
  return body as T
}

export const getRuntime = () => api('/api/runtime')
export const getProjects = () => api('/api/projects')
export const getTasks = () => api('/api/tasks')
export const getUsage = (projectId = '') => api(`/api/usage${projectId ? `?project_id=${encodeURIComponent(projectId)}` : ''}`)
export const getTaskStatus = (id: string) => api(`/api/tasks/${encodeURIComponent(id)}/status`)
export const getTaskResult = (id: string) => api(`/api/tasks/${encodeURIComponent(id)}/result`)
export const getTaskInspect = (id: string) => api(`/api/tasks/${encodeURIComponent(id)}/inspect`)
export const getTaskFeedback = (id: string) => api(`/api/tasks/${encodeURIComponent(id)}/feedback`)

export async function createTask(payload: Json) {
  return api('/api/tasks', { method: 'POST', body: JSON.stringify(payload) })
}
export async function cancelTask(id: string) {
  return api(`/api/tasks/${encodeURIComponent(id)}/cancel`, { method: 'POST', body: '{}' })
}
export async function inputTask(id: string, message: string) {
  return api(`/api/tasks/${encodeURIComponent(id)}/input`, { method: 'POST', body: JSON.stringify({ message }) })
}
export async function feedbackTask(id: string, verdict: string, message: string) {
  return api(`/api/tasks/${encodeURIComponent(id)}/feedback`, { method: 'POST', body: JSON.stringify({ verdict, message }) })
}
export async function browseProjectFolders(path = '') {
  return api('/api/projects/browse', { method: 'POST', body: JSON.stringify({ path }) })
}
export async function pickProject() {
  const controller = new AbortController()
  const timeout = window.setTimeout(() => controller.abort(), 120_000)
  try {
    return await api('/api/projects/pick', { method: 'POST', body: '{}', signal: controller.signal })
  } catch (err: any) {
    if (err?.name === 'AbortError') throw new Error('Hộp thoại chọn thư mục không phản hồi. Hãy thử lại hoặc nhập đường dẫn trực tiếp.')
    throw err
  } finally {
    window.clearTimeout(timeout)
  }
}
export async function addProject(root: string, id = '') {
  return api('/api/projects', { method: 'POST', body: JSON.stringify({ root, id }) })
}
export async function updateProjectPolicy(id: string, local_file_write: boolean, local_git_write: boolean, network_allowed: boolean, remote_git_write: boolean, deploy_allowed: boolean) {
  return api(`/api/projects/${encodeURIComponent(id)}/policy`, { method: 'POST', body: JSON.stringify({ local_file_write, local_git_write, network_allowed, remote_git_write, deploy_allowed }) })
}
export async function sandboxPrepare() {
  return api('/api/runtime/sandbox/prepare', { method: 'POST', body: '{}' })
}
export async function connectionAction(path: string, body: Json = {}) {
  return api(path, { method: 'POST', body: JSON.stringify(body) })
}

export function fmtNumber(value: any) {
  return new Intl.NumberFormat('vi-VN').format(Number(value || 0))
}
export function fmtTime(value: any) {
  if (!value) return '—'
  try { return new Date(value).toLocaleString('vi-VN') } catch { return String(value) }
}
export function fmtDuration(seconds: any) {
  const s = Math.max(0, Number(seconds || 0))
  if (s < 60) return `${Math.floor(s)}s`
  if (s < 3600) return `${Math.floor(s / 60)}m`
  if (s < 86400) return `${Math.floor(s / 3600)}h ${Math.floor((s % 3600) / 60)}m`
  return `${Math.floor(s / 86400)}d ${Math.floor((s % 86400) / 3600)}h`
}
