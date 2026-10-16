/** The server wraps every reply as {code, reason, msg, data}; success is code 200. */
interface Envelope<T> {
  code: number
  reason: string
  msg: string
  data: T
}

/**
 * A failed call. `status` is the HTTP status (0 for a network failure); `reason` is the
 * server's i18n key such as "purchase.vote_closed", or "network_error" / "bad_response"
 * for failures that never reached the server's own error path. `data` keeps the field
 * errors the server attaches to validation failures.
 */
export class ApiError extends Error {
  readonly status: number
  readonly reason: string
  readonly data: unknown

  constructor(status: number, reason: string, message: string, data: unknown = null) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.reason = reason
    this.data = data
  }
}

export interface ApiClientOptions {
  baseUrl: string
  getToken: () => string | null
  /** called on a 401, so the app can drop the stale token and go to the login page */
  onUnauthorized: () => void
  fetch?: typeof fetch
}

export type Query = Record<string, string | number | boolean | undefined>

export interface ApiClient {
  get<T>(path: string, query?: Query, init?: RequestInit): Promise<T>
  post<T>(path: string, body?: unknown, init?: RequestInit): Promise<T>
  upload<T>(path: string, form: FormData, init?: RequestInit): Promise<T>
}

const isAbort = (e: unknown) => e instanceof DOMException && e.name === 'AbortError'

export function createApiClient(opts: ApiClientOptions): ApiClient {
  const doFetch = opts.fetch ?? ((...args) => fetch(...args))

  function buildUrl(path: string, query?: Query) {
    const params = new URLSearchParams()
    for (const [k, v] of Object.entries(query ?? {})) {
      if (v !== undefined) params.append(k, String(v))
    }
    const qs = params.toString()
    return `${opts.baseUrl}${path}${qs ? `?${qs}` : ''}`
  }

  async function request<T>(url: string, init: RequestInit): Promise<T> {
    const headers = new Headers(init.headers)
    const token = opts.getToken()
    if (token) headers.set('Authorization', token)

    let res: Response
    try {
      res = await doFetch(url, { ...init, headers })
    } catch (e) {
      if (isAbort(e)) throw e
      throw new ApiError(0, 'network_error', '网络连接失败，请稍后重试')
    }

    let body: Envelope<T>
    try {
      body = (await res.json()) as Envelope<T>
    } catch {
      throw new ApiError(res.status, 'bad_response', '服务器返回了无法识别的内容')
    }

    if (res.ok && body.code === 200) return body.data

    const status = res.ok ? body.code : res.status
    if (status === 401) opts.onUnauthorized()
    throw new ApiError(status, body.reason, body.msg, body.data)
  }

  return {
    get: (path, query, init) => request(buildUrl(path, query), { ...init, method: 'GET' }),
    post: (path, body, init) => {
      const headers = new Headers(init?.headers)
      headers.set('Content-Type', 'application/json')
      return request(buildUrl(path), {
        ...init,
        method: 'POST',
        headers,
        body: body === undefined ? undefined : JSON.stringify(body),
      })
    },
    upload: (path, form, init) => request(buildUrl(path), { ...init, method: 'POST', body: form }),
  }
}
