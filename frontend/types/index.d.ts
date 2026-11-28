import type { Component, DefineComponent } from 'vue'

export interface PoWQuestion {
  algo: 'sha256'
  prefix: string
  bits: number
}

export type ChallengeMode = 'puzzle' | 'plain'

export interface BehaviorSignals {
  pointerType?: string
  events?: number
  trusted?: number
  coalesced?: number
  keyboard?: boolean
}

export interface Challenge {
  id: string
  mode?: ChallengeMode
  background: string
  piece: string
  width: number
  height: number
  pieceX: number
  pieceY: number
  pieceWidth: number
  pieceHeight: number
  decoys?: number
  tolerance: number
  expiresIn: number
  pow?: PoWQuestion
}

export interface TrailPoint {
  t: number
  x: number
  y: number
  p?: number
}

export interface VerifyPayload {
  id: string
  x: number
  y?: number
  trail?: TrailPoint[]
  nonce?: string
  behavior?: BehaviorSignals
}

export interface VerifyResponse {
  ok: boolean
  ticket?: string
  code?: string
  reason?: string
  retry?: boolean
  score?: number
  next?: 'plain'
  nextId?: string
}

export interface CaptchaTexts {
  title: string
  hint: string
  plainHint: string
  plainHeader: string
  plainTitle: string
  decoyHint: string
  success: string
  fail: string
  loading: string
  refresh: string
  networkError: string
  tooManyRequests: string
}

export interface CaptchaClientOptions {
  apiBase?: string
  headers?: HeadersInit
  requestInit?: RequestInit
}

export interface ShanheCaptchaProps {
  apiBase?: string
  width?: number
  theme?: 'light' | 'dark' | 'auto'
  disabled?: boolean
  autoLoad?: boolean
  texts?: Partial<CaptchaTexts>
  headers?: HeadersInit
  requestInit?: RequestInit
}

export interface ShanheCaptchaExpose {
  refresh(): Promise<void>
  reset(): void
  getTicket(): string
}

export interface ShanheCaptchaEmits {
  stage: (stage: 'puzzle' | 'plain') => void
  success: (ticket: string) => void
  fail: (code: string) => void
  refresh: () => void
  error: (err: unknown) => void
}

export declare const ShanheCaptcha: DefineComponent<
  ShanheCaptchaProps,
  ShanheCaptchaExpose,
  {},
  {},
  {},
  {},
  {},
  ShanheCaptchaEmits
>

export declare const defaultTexts: CaptchaTexts

export declare class CaptchaError extends Error {
  status: number
  constructor(message: string, status?: number)
}

export declare function fetchChallenge(options?: CaptchaClientOptions): Promise<Challenge>

export declare function captchaClientHeader(): Record<string, string>
export declare function verifyChallenge(
  payload: VerifyPayload,
  options?: CaptchaClientOptions,
): Promise<VerifyResponse>

export declare function solvePoW(prefix: string, bits: number, deadlineMs?: number): Promise<string>

declare const plugin: {
  install(app: import('vue').App): void
}

export default plugin

export type ShanheCaptchaComponent = Component
