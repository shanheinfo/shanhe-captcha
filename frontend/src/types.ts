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
  /** puzzle：拼图挑战；plain：第二道无图滑块 */
  mode?: ChallengeMode
  /** data:image/jpeg;base64,... 背景图（已绘制缺口）；plain 模式下为空 */
  background: string

  piece: string
  width: number
  height: number
  pieceX: number
  pieceY: number
  pieceWidth: number
  pieceHeight: number
  /** 图中假缺口数量（仅用于提示，不参与判定） */
  decoys?: number
  tolerance: number
  expiresIn: number
  pow?: PoWQuestion
}

export interface TrailPoint {
  /** 相对拖拽开始的毫秒数 */
  t: number
  /** 相对拖拽起点的横向位移（图片像素） */
  x: number
  y: number

  p?: number
}

export interface VerifyPayload {
  id: string
  x: number
  y?: number
  /** 客户端上报耗时（毫秒） */
  duration: number
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
  /** 第二道验证：next="plain" 时使用 nextId 发起第二轮拖动 */
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

export const defaultTexts: CaptchaTexts = {
  title: '安全验证',
  hint: '向右拖动滑块完成拼图',
  plainHint: '按住滑块，拖到最右侧',
  plainHeader: '最后一步',
  decoyHint: '多个缺口，按背景边缘选择',
  plainTitle: '最后一步：拖到最右侧',
  success: '验证通过',
  fail: '验证失败，请重试',
  loading: '加载中',
  refresh: '换一张',
  networkError: '网络异常，请重试',
  tooManyRequests: '操作过于频繁，稍后再试',
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

export interface CaptchaClientOptions {
  apiBase?: string
  headers?: HeadersInit
  requestInit?: RequestInit
}
