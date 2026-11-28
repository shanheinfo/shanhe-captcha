import type {
  CaptchaClientOptions,
  Challenge,
  VerifyPayload,
  VerifyResponse,
} from './types'

const CLIENT_ID_KEY = 'shanhe-captcha-client-id'

function getClientId(): string {
  try {
    let id = localStorage.getItem(CLIENT_ID_KEY)
    if (!id) {
      const bytes = new Uint8Array(12)
      crypto.getRandomValues(bytes)
      id = Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('')
      localStorage.setItem(CLIENT_ID_KEY, id)
    }
    return id
  } catch {
    return ''
  }
}

/** 业务请求也要带这个头，消费凭证时会核对。 */
export function captchaClientHeader(): Record<string, string> {
  const id = getClientId()
  return id ? { 'X-Captcha-Client': id } : {}
}

function normalizeBase(base: string): string {
  return base.replace(/\/+$/, '')
}

function mergeHeaders(...sources: Array<HeadersInit | undefined>): Headers {
  const merged = new Headers()
  for (const source of sources) {
    if (!source) continue
    new Headers(source).forEach((value, key) => merged.set(key, value))
  }
  return merged
}

async function request<T>(
  url: string,
  init: RequestInit,
  options: CaptchaClientOptions,
): Promise<T> {
  const headers = mergeHeaders(options.headers, options.requestInit?.headers, init.headers)
  const clientId = getClientId()
  if (clientId) headers.set('X-Captcha-Client', clientId)

  const resp = await fetch(url, {
    credentials: 'same-origin',
    ...options.requestInit,
    ...init,
    headers,
  })

  if (!resp.ok) {
    throw new CaptchaError(`captcha http ${resp.status}`, resp.status)
  }
  return (await resp.json()) as T
}

export class CaptchaError extends Error {
  status: number
  constructor(message: string, status = 0) {
    super(message)
    this.name = 'CaptchaError'
    this.status = status
  }
}

export async function fetchChallenge(options: CaptchaClientOptions = {}): Promise<Challenge> {
  const base = normalizeBase(options.apiBase ?? '/captcha')
  return request<Challenge>(`${base}/challenge`, { method: 'GET', cache: 'no-store' }, options)
}

export async function verifyChallenge(
  payload: VerifyPayload,
  options: CaptchaClientOptions = {},
): Promise<VerifyResponse> {
  const base = normalizeBase(options.apiBase ?? '/captcha')
  return request<VerifyResponse>(
    `${base}/verify`,
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    },
    options,
  )
}
