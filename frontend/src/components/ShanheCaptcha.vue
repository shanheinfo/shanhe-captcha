<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue'
import { CaptchaError, fetchChallenge, verifyChallenge } from '../client'
import { solvePoW } from '../sha256'
import { defaultTexts } from '../types'
import type { BehaviorSignals, CaptchaTexts, Challenge, TrailPoint } from '../types'

type State = 'idle' | 'loading' | 'ready' | 'dragging' | 'verifying' | 'success' | 'fail' | 'error'

const HANDLE = 44

const props = withDefaults(
  defineProps<{
    apiBase?: string
    width?: number
    theme?: 'light' | 'dark' | 'auto'
    disabled?: boolean
    autoLoad?: boolean
    texts?: Partial<CaptchaTexts>
    headers?: HeadersInit
    requestInit?: RequestInit
  }>(),
{
  apiBase: '/captcha',
  width: 344,
  theme: 'light',
  disabled: false,
  autoLoad: true,
})

const emit = defineEmits<{
  (e: 'stage', stage: 'puzzle' | 'plain'): void
  (e: 'success', ticket: string): void
  (e: 'fail', code: string): void
  (e: 'refresh'): void
  (e: 'error', err: unknown): void
}>()

const t = computed<CaptchaTexts>(() => ({ ...defaultTexts, ...props.texts }))

const state = ref<State>('idle')
const challenge = shallowRef<Challenge | null>(null)
const offset = ref(0)
const scale = ref(1)
const frameWidth = ref(320)
const errorMsg = ref('')
const ticket = ref('')
const frameEl = ref<HTMLDivElement | null>(null)
const trackEl = ref<HTMLDivElement | null>(null)
const trackWidth = ref(320)
const pointerType = ref('mouse')
const evCount = ref(0)
const trustedCount = ref(0)
const coalescedCount = ref(0)

const systemDark = ref(false)
let mql: MediaQueryList | null = null
let onSystemTheme = () => {}

const resolvedTheme = computed(() => {
  if (props.theme === 'auto') return systemDark.value ? 'dark' : 'light'
  return props.theme
})

const rootClass = computed(() => [
  `shc-theme-${resolvedTheme.value}`,
  {
    'is-disabled': props.disabled,
    'is-dragging': state.value === 'dragging',
    'is-fail': state.value === 'fail',
    'is-success': state.value === 'success',
  },
])

const isPlain = computed(() => challenge.value?.mode === 'plain')
const hasImage = computed(() => !!challenge.value && !isPlain.value && !!challenge.value.background)

const hintText = computed(() => {
  if (state.value === 'loading' || state.value === 'idle') return t.value.loading
  if (isPlain.value) return t.value.plainHint
  if ((challenge.value?.decoys ?? 0) > 0) return t.value.decoyHint
  return t.value.hint
})

const maxOffset = computed(() => {
  const c = challenge.value
  if (!c) return 0
  if (isPlain.value) return 100
  const byImage = c.width - c.pieceX - c.pieceWidth
  const byTrack = (frameWidth.value - HANDLE) / (scale.value || 1)
  return Math.max(0, Math.min(byImage, byTrack))
})

const handleX = computed(() => {
  if (isPlain.value) {
    const usable = Math.max(0, trackWidth.value - HANDLE)
    return Math.min((offset.value / 100) * usable, usable)
  }
  return Math.min(offset.value * scale.value, Math.max(0, frameWidth.value - HANDLE))
})

const frameStyle = computed(() => {
  const c = challenge.value
  if (!c) return { height: '160px' }
  return { aspectRatio: `${c.width} / ${c.height}` }
})

const pieceStyle = computed(() => {
  const c = challenge.value
  if (!c) return {}
  const k = scale.value
  return {
    width: `${c.pieceWidth * k}px`,
    height: `${c.pieceHeight * k}px`,
    backgroundImage: `url("${c.piece}")`,
    transform: `translate3d(${(c.pieceX + offset.value) * k}px, ${c.pieceY * k}px, 0)`,
  }
})

let loadedAt = 0
let abort: AbortController | null = null
let failTimer: number | null = null
let loadSeq = 0
let dragging = false
let dragStartX = 0
let dragStartY = 0
let dragStartOffset = 0
let dragStartTime = 0
let trail: TrailPoint[] = []
let usedKeyboard = false

function clamp(v: number, min: number, max: number): number {
  return v < min ? min : v > max ? max : v
}

function measure() {
  if (trackEl.value) trackWidth.value = trackEl.value.clientWidth
  const el = frameEl.value
  if (!el || !challenge.value || isPlain.value) return
  frameWidth.value = el.clientWidth
  scale.value = el.clientWidth / challenge.value.width
}

function isAbortError(err: unknown): boolean {
  return (err as DOMException)?.name === 'AbortError'
}

function isRateLimited(err: unknown): boolean {
  return err instanceof CaptchaError && err.status === 429
}

function errorText(err: unknown): string {
  return isRateLimited(err) ? t.value.tooManyRequests : t.value.networkError
}

async function refresh() {
  if (props.disabled) return
  clearFailTimer()
  abort?.abort()
  abort = new AbortController()
  const seq = ++loadSeq
  dragging = false
  detach()

  state.value = 'loading'
  challenge.value = null
  offset.value = 0
  trail = []
  ticket.value = ''
  errorMsg.value = ''
  evCount.value = 0
  trustedCount.value = 0
  coalescedCount.value = 0
  usedKeyboard = false
  pointerType.value = 'mouse'
  emit('refresh')

  try {
    const ch = await fetchChallenge({
      apiBase: props.apiBase,
      headers: props.headers,
      requestInit: { ...props.requestInit, signal: abort.signal },
    })
    if (seq !== loadSeq) return
    challenge.value = ch
    loadedAt = performance.now()
    state.value = 'ready'
    await nextTick()
    measure()
  } catch (err) {
    if (seq !== loadSeq || isAbortError(err)) return
    state.value = 'error'
    errorMsg.value = errorText(err)
    emit('error', err)
  }
}

function reset() {
  clearFailTimer()
  abort?.abort()
  loadSeq++
  dragging = false
  detach()
  usedKeyboard = false
  offset.value = 0
  trail = []
  ticket.value = ''
  if (challenge.value) state.value = 'ready'
}

function clearFailTimer() {
  if (failTimer !== null) {
    window.clearTimeout(failTimer)
    failTimer = null
  }
}

function detach() {
  window.removeEventListener('pointermove', onPointerMove)
  window.removeEventListener('pointerup', onPointerUp)
  window.removeEventListener('pointercancel', onPointerUp)
}

function onPointerDown(e: PointerEvent) {
  if (props.disabled || state.value !== 'ready') return
  e.preventDefault()
  // 键盘移开后再用鼠标，先把滑块放回起点。
  if (usedKeyboard) offset.value = 0
  dragging = true
  usedKeyboard = false
  state.value = 'dragging'
  dragStartX = e.clientX
  dragStartY = e.clientY
  dragStartOffset = offset.value
  dragStartTime = performance.now()
  pointerType.value = e.pointerType || 'mouse'
  evCount.value = 0
  trustedCount.value = 0
  coalescedCount.value = 0
  trail = [{ t: 0, x: offset.value, y: 0, p: e.pressure || undefined }]
  window.addEventListener('pointermove', onPointerMove, { passive: false })
  window.addEventListener('pointerup', onPointerUp)
  window.addEventListener('pointercancel', onPointerUp)
}

function onPointerMove(e: PointerEvent) {
  if (!dragging) return
  e.preventDefault()

  // 有的浏览器会返回空数组，这时直接用原事件。
  const coalesced = typeof e.getCoalescedEvents === 'function' ? e.getCoalescedEvents() : []
  const events = coalesced.length > 0 ? coalesced : [e]
  evCount.value++
  if (e.isTrusted) trustedCount.value++
  coalescedCount.value += coalesced.length
  for (const ev of events) {
    const delta = isPlain.value
      ? ((ev.clientX - dragStartX) / Math.max(1, trackWidth.value - HANDLE)) * 100
      : (ev.clientX - dragStartX) / (scale.value || 1)
    const deltaY = isPlain.value
      ? ev.clientY - dragStartY
      : (ev.clientY - dragStartY) / (scale.value || 1)
    const next = clamp(dragStartOffset + delta, 0, maxOffset.value)
    offset.value = next
    if (trail.length < 2000) {
      trail.push({
        t: Math.round(performance.now() - dragStartTime),
        x: Number(next.toFixed(2)),
        y: Number(deltaY.toFixed(2)),
        p: ev.pressure || undefined,
      })
    }
  }
}

function onPointerUp() {
  if (!dragging) return
  dragging = false
  detach()
  if (isPlain.value && offset.value >= 90) {
    offset.value = 100
    // 补一个终点，和服务端看到的最后位置保持一致。
    trail.push({ t: Math.round(performance.now() - dragStartTime), x: 100, y: 0 })
  } else if (!isPlain.value && offset.value >= 2) {
    // 长拖拽可能已到 2000 点上限，补一个终点。
    trail.push({ t: Math.round(performance.now() - dragStartTime), x: offset.value, y: 0 })
  }
  if (offset.value < (isPlain.value ? 95 : 2)) {
    // 中途松开就回到起点，否则下一次轨迹首点不对。
    if (isPlain.value) offset.value = 0
    state.value = 'ready'
    return
  }
  void submit()
}

function onKeyDown(e: KeyboardEvent) {
  if (props.disabled || state.value !== 'ready') return
  const step = e.shiftKey ? 10 : 2

  if (e.key === 'ArrowRight' || e.key === 'ArrowLeft') {
    e.preventDefault()
    usedKeyboard = true
    const dir = e.key === 'ArrowRight' ? 1 : -1
    offset.value = clamp(offset.value + dir * step, 0, maxOffset.value)
    return
  }
  if (e.key === 'Enter' || e.key === ' ') {
    e.preventDefault()
    usedKeyboard = true
    pointerType.value = 'keyboard'
    if (offset.value >= (isPlain.value ? 95 : 2)) void submit()
  }
}

// 键盘没有指针轨迹，生成一条直线。
function keyboardTrail(dest: number): TrailPoint[] {
  const n = 24
  const out: TrailPoint[] = []
  // 首点必须是 0。
  out.push({ t: 0, x: 0, y: 0 })
  for (let i = 1; i <= n; i++) {
    out.push({ t: i * 16, x: Number(((dest * i) / n).toFixed(2)), y: 0 })
  }
  return out
}

function enterPlainStage(id: string) {
  clearFailTimer()
  loadSeq++
  usedKeyboard = false
  pointerType.value = 'mouse'
  evCount.value = 0
  trustedCount.value = 0
  coalescedCount.value = 0
  challenge.value = {
    id,
    mode: 'plain',
    background: '',
    piece: '',
    width: 0,
    height: 0,
    pieceX: 0,
    pieceY: 0,
    pieceWidth: 0,
    pieceHeight: 0,
    tolerance: 2,
    expiresIn: 60,
  }
  state.value = 'ready'
  offset.value = 0
  trail = []
  ticket.value = ''
  loadedAt = performance.now()
  void nextTick().then(measure)
  emit('stage', 'plain')
}

async function submit() {
  const c = challenge.value
  if (!c) return
  const seq = loadSeq
  state.value = 'verifying'

  try {
    let nonce: string | undefined
    if (c.pow && c.pow.bits > 0) {
      nonce = await solvePoW(c.pow.prefix, c.pow.bits)
      if (!nonce) throw new Error('pow timeout')
    }

    const behavior: BehaviorSignals = usedKeyboard
      ? { pointerType: 'keyboard', keyboard: true }
      : {
          pointerType: pointerType.value || 'mouse',
          events: evCount.value,
          trusted: trustedCount.value,
          coalesced: coalescedCount.value,
        }

    const payload = {
      id: c.id,
      x: offset.value,
      duration: Math.round(performance.now() - loadedAt),
      trail: usedKeyboard ? keyboardTrail(offset.value) : trail,
      nonce,
      behavior,
    }
    const res = await verifyChallenge(payload, {
      apiBase: props.apiBase,
      headers: props.headers,
      requestInit: { ...props.requestInit, signal: abort?.signal },
    })
    if (seq !== loadSeq) return

    if (res.ok && res.next === 'plain' && res.nextId) {
      enterPlainStage(res.nextId)
      return
    }

    if (res.ok && res.ticket) {
      ticket.value = res.ticket
      state.value = 'success'
      emit('success', res.ticket)
      return
    }

    state.value = 'fail'
    emit('fail', res.code ?? 'unknown')
    failTimer = window.setTimeout(() => {
      failTimer = null
      if (state.value === 'fail') void refresh()
    }, 1100)
  } catch (err) {
    if (seq !== loadSeq || isAbortError(err)) return
    state.value = 'error'
    errorMsg.value = errorText(err)
    emit('error', err)
  }
}

let ro: ResizeObserver | null = null

onMounted(() => {
  if (typeof window !== 'undefined' && window.matchMedia) {
    mql = window.matchMedia('(prefers-color-scheme: dark)')
    systemDark.value = mql.matches
    onSystemTheme = () => {
      systemDark.value = mql!.matches
    }
    mql.addEventListener('change', onSystemTheme)
  }
  if (typeof ResizeObserver !== 'undefined') {
    ro = new ResizeObserver(() => measure())
    if (frameEl.value) ro.observe(frameEl.value)
    if (trackEl.value) ro.observe(trackEl.value)
  }
  window.addEventListener('resize', measure)
  if (props.autoLoad) void refresh()
})

onBeforeUnmount(() => {
  clearFailTimer()
  abort?.abort()
  detach()
  window.removeEventListener('resize', measure)
  ro?.disconnect()
  if (mql && onSystemTheme) mql.removeEventListener('change', onSystemTheme)
})

watch(
  () => props.disabled,
  (v) => {
    if (v) {
      dragging = false
      detach()
      if (state.value === 'dragging') state.value = 'ready'
    }
  },
)

watch(() => props.apiBase, () => {
  if (props.autoLoad && !props.disabled) void refresh()
})

defineExpose({ refresh, reset, getTicket: () => ticket.value })
</script>

<template>
  <div class="shc-root" :class="rootClass" :style="{ width: `${width}px` }">
    <div class="shc-header">
      <span class="shc-title">
        <svg class="shc-icon" viewBox="0 0 24 24" aria-hidden="true">
          <path d="M12 3.2 4.8 5.6v6.1c0 4.4 3 7.6 7.2 9.1 4.2-1.5 7.2-4.7 7.2-9.1V5.6L12 3.2Z" />
          <path d="M8.9 12.1l2.2 2.2 4-4.4" />
        </svg>
        {{ isPlain ? t.plainHeader : t.title }}
      </span>
      <button
        class="shc-refresh"
        :class="{ spinning: state === 'loading' }"
        type="button"
        :disabled="disabled || state === 'verifying'"
        :title="t.refresh"
        :aria-label="t.refresh"
        @click="refresh"
      >
        <svg viewBox="0 0 24 24" aria-hidden="true">
          <path d="M20 11.5a8 8 0 1 1-2.4-5.7" />
          <path d="M20 4.5v5h-5" />
        </svg>
      </button>
    </div>

    <div v-if="hasImage" ref="frameEl" class="shc-frame" :style="frameStyle">
      <img
        v-if="challenge"
        class="shc-bg"
        :src="challenge.background"
        alt=""
        draggable="false"
        @load="measure"
      />
      <div v-if="hasImage" class="shc-piece" :style="pieceStyle" />

      <div v-if="state === 'loading' || state === 'idle'" class="shc-layer shc-layer-loading">
        <span class="shc-spinner" />
      </div>

      <div v-if="state === 'success'" class="shc-layer shc-layer-success">
        <span class="shc-check">
          <svg viewBox="0 0 24 24" aria-hidden="true">
            <path d="M5.2 12.6 9.4 16.8 18.8 7.4" />
          </svg>
        </span>
        <span class="shc-success-text">{{ t.success }}</span>
      </div>

      <div v-if="state === 'error'" class="shc-layer shc-layer-error">{{ errorMsg }}</div>
    </div>

    <div v-if="state === 'error' && !hasImage && !isPlain" class="shc-plain-panel">
      <span class="shc-plain-error">{{ errorMsg }}</span>
    </div>

    <div v-if="isPlain" class="shc-plain-panel">
      <template v-if="state === 'success'">
        <span class="shc-check">
          <svg viewBox="0 0 24 24" aria-hidden="true">
            <path d="M5.2 12.6 9.4 16.8 18.8 7.4" />
          </svg>
        </span>
        <span class="shc-success-text">{{ t.success }}</span>
      </template>
      <template v-else-if="state === 'error'">
        <span class="shc-plain-error">{{ errorMsg }}</span>
      </template>
      <template v-else>
        <svg class="shc-plain-icon" viewBox="0 0 24 24" aria-hidden="true">
          <rect x="4" y="6" width="16" height="12" rx="2" />
          <path d="M8 12h8" />
          <path d="M13.5 9.5 16 12l-2.5 2.5" />
        </svg>
        <span class="shc-plain-title">{{ t.plainTitle }}</span>
      </template>
    </div>

    <div ref="trackEl" class="shc-track" :class="{ active: state === 'dragging', success: state === 'success', fail: state === 'fail' }">
      <div class="shc-track-fill" :style="{ width: `${handleX + HANDLE / 2}px` }" />
      <span v-if="state !== 'success' && state !== 'verifying'" class="shc-track-text">
        {{ hintText }}
      </span>
      <div
        class="shc-handle"
        :style="{ transform: `translateX(${handleX}px)` }"
        role="slider"
        :aria-label="t.title"
        :aria-valuemin="0"
        :aria-valuemax="Math.round(maxOffset)"
        :aria-valuenow="Math.round(offset)"
        :tabindex="disabled || state !== 'ready' ? -1 : 0"
        @pointerdown="onPointerDown"
        @keydown="onKeyDown"
      >
        <svg v-if="state === 'success'" class="shc-handle-icon" viewBox="0 0 24 24" aria-hidden="true">
          <path d="M5.2 12.6 9.4 16.8 18.8 7.4" />
        </svg>
        <svg v-else class="shc-handle-icon" viewBox="0 0 24 24" aria-hidden="true">
          <path d="M5 12h13" />
          <path d="M13.5 7.5 18 12l-4.5 4.5" />
        </svg>
      </div>
    </div>
  </div>
</template>

<style>
.shc-root {
  --shc-bg: #ffffff;
  --shc-text: #1f2733;
  --shc-text-2: #69727f;
  --shc-track: #f1f4f8;
  --shc-hint: #9aa5b3;
  --shc-border: rgba(15, 23, 42, 0.08);
  --shc-accent: #2563eb;
  --shc-accent-soft: rgba(37, 99, 235, 0.16);
  --shc-success: #16a34a;

  box-sizing: border-box;
  width: 100%;
  max-width: 100%;
  padding: 12px;
  border-radius: 14px;
  background: var(--shc-bg);
  border: 1px solid var(--shc-border);
  box-shadow: 0 6px 24px rgba(15, 23, 42, 0.06);
  font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', 'PingFang SC', 'Hiragino Sans GB',
    'Microsoft YaHei', sans-serif;
  user-select: none;
  -webkit-user-select: none;
  -webkit-tap-highlight-color: transparent;
  transition: box-shadow 0.2s ease, border-color 0.2s ease;
}

.shc-root.shc-theme-dark {
  --shc-bg: #171c23;
  --shc-text: #e6edf3;
  --shc-text-2: #9aa5b3;
  --shc-track: #232b36;
  --shc-hint: #7b8797;
  --shc-border: rgba(255, 255, 255, 0.08);
  --shc-accent: #60a5fa;
  --shc-accent-soft: rgba(96, 165, 250, 0.2);
  --shc-success: #4ade80;
  box-shadow: 0 8px 28px rgba(0, 0, 0, 0.35);
}

.shc-root.is-disabled {
  opacity: 0.6;
  pointer-events: none;
}

.shc-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  height: 24px;
  margin-bottom: 10px;
}

.shc-title {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  font-weight: 500;
  color: var(--shc-text-2);
  letter-spacing: 0.2px;
}

.shc-icon {
  width: 15px;
  height: 15px;
  fill: none;
  stroke: var(--shc-accent);
  stroke-width: 1.6;
  stroke-linecap: round;
  stroke-linejoin: round;
}

.shc-refresh {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 26px;
  height: 26px;
  padding: 0;
  border: none;
  border-radius: 8px;
  background: transparent;
  color: var(--shc-text-2);
  cursor: pointer;
  transition: background 0.16s ease, color 0.16s ease;
}

.shc-refresh:hover:not(:disabled) {
  background: var(--shc-accent-soft);
  color: var(--shc-accent);
}

.shc-refresh:disabled {
  cursor: not-allowed;
  opacity: 0.5;
}

.shc-refresh svg {
  width: 16px;
  height: 16px;
  fill: none;
  stroke: currentColor;
  stroke-width: 1.8;
  stroke-linecap: round;
  stroke-linejoin: round;
}

.shc-refresh.spinning svg {
  animation: shc-spin 0.9s linear infinite;
}

.shc-frame {
  position: relative;
  width: 100%;
  border-radius: 10px;
  overflow: hidden;
  background: #e8ecf1;
  box-shadow: inset 0 0 0 1px rgba(15, 23, 42, 0.06);
}

.shc-bg {
  display: block;
  width: 100%;
  height: 100%;
  object-fit: fill;
  pointer-events: none;
}

.shc-piece {
  position: absolute;
  top: 0;
  left: 0;
  background-repeat: no-repeat;
  background-size: 100% 100%;
  filter: drop-shadow(0 3px 6px rgba(0, 0, 0, 0.34));
  will-change: transform;
  pointer-events: none;
  transition: transform 0.18s cubic-bezier(0.22, 1, 0.36, 1);
}

.shc-root.is-dragging .shc-piece {
  filter: drop-shadow(0 6px 10px rgba(0, 0, 0, 0.4));
  transition: none;
}

.shc-layer {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 10px;
  z-index: 3;
}

.shc-layer-loading {
  background: rgba(240, 243, 247, 0.72);
  backdrop-filter: blur(2px);
}

.shc-theme-dark .shc-layer-loading {
  background: rgba(23, 28, 35, 0.7);
}

.shc-spinner {
  width: 22px;
  height: 22px;
  border-radius: 50%;
  border: 2px solid rgba(37, 99, 235, 0.22);
  border-top-color: var(--shc-accent);
  animation: shc-spin 0.8s linear infinite;
}

.shc-layer-success {
  flex-direction: column;
  background: rgba(255, 255, 255, 0.82);
  backdrop-filter: blur(2px);
}

.shc-theme-dark .shc-layer-success {
  background: rgba(23, 28, 35, 0.78);
}

.shc-check {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 34px;
  height: 34px;
  border-radius: 50%;
  background: var(--shc-success);
  box-shadow: 0 4px 12px rgba(22, 163, 74, 0.35);
}

.shc-check svg {
  width: 20px;
  height: 20px;
  fill: none;
  stroke: #fff;
  stroke-width: 2.4;
  stroke-linecap: round;
  stroke-linejoin: round;
  stroke-dasharray: 24;
  stroke-dashoffset: 24;
  animation: shc-draw 0.32s ease forwards 0.08s;
}

.shc-success-text {
  font-size: 13px;
  font-weight: 500;
  color: var(--shc-success);
}

.shc-layer-error {
  flex-direction: column;
  gap: 6px;
  font-size: 12px;
  color: var(--shc-text-2);
}

.shc-plain-panel {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  height: 120px;
  margin-top: 2px;
  border-radius: 10px;
  background:
    radial-gradient(120% 120% at 20% 0%, var(--shc-accent-soft), transparent 60%),
    var(--shc-track);
  box-shadow: inset 0 0 0 1px rgba(15, 23, 42, 0.05);
}

.shc-theme-dark .shc-plain-panel {
  box-shadow: inset 0 0 0 1px rgba(255, 255, 255, 0.06);
}

.shc-plain-icon {
  width: 34px;
  height: 34px;
  fill: none;
  stroke: var(--shc-accent);
  stroke-width: 1.6;
  stroke-linecap: round;
  stroke-linejoin: round;
  opacity: 0.9;
}

.shc-plain-title {
  font-size: 13px;
  color: var(--shc-text-2);
}

.shc-plain-error {
  max-width: 240px;
  font-size: 12px;
  line-height: 1.5;
  color: #dc2626;
  text-align: center;
}

.shc-track {
  position: relative;
  height: 44px;
  margin-top: 10px;
  border-radius: 10px;
  background: var(--shc-track);
  overflow: hidden;
  box-shadow: inset 0 0 0 1px rgba(15, 23, 42, 0.05);
}

.shc-theme-dark .shc-track {
  box-shadow: inset 0 0 0 1px rgba(255, 255, 255, 0.05);
}

.shc-track-fill {
  position: absolute;
  top: 0;
  bottom: 0;
  left: 0;
  background: linear-gradient(90deg, var(--shc-accent-soft), rgba(37, 99, 235, 0.02));
  transition: width 0.1s linear;
}

.shc-track.success .shc-track-fill {
  background: linear-gradient(90deg, rgba(22, 163, 74, 0.22), rgba(22, 163, 74, 0.04));
}

.shc-track-text {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 12px;
  color: var(--shc-hint);
  letter-spacing: 0.4px;
  pointer-events: none;
}

.shc-handle {
  position: absolute;
  top: 0;
  left: 0;
  z-index: 2;
  display: flex;
  align-items: center;
  justify-content: center;
  width: 44px;
  height: 44px;
  border-radius: 10px;
  background: var(--shc-bg);
  box-shadow: 0 1px 3px rgba(15, 23, 42, 0.2), inset 0 0 0 1px rgba(15, 23, 42, 0.06);
  cursor: grab;
  touch-action: none;
  outline: none;
  transition: transform 0.18s cubic-bezier(0.22, 1, 0.36, 1), box-shadow 0.16s ease;
}

.shc-theme-dark .shc-handle {
  box-shadow: 0 2px 6px rgba(0, 0, 0, 0.5), inset 0 0 0 1px rgba(255, 255, 255, 0.08);
}

.shc-handle:hover {
  box-shadow: 0 2px 8px rgba(15, 23, 42, 0.26), inset 0 0 0 1px rgba(37, 99, 235, 0.24);
}

.shc-handle:focus-visible {
  box-shadow: 0 0 0 3px var(--shc-accent-soft), inset 0 0 0 1px var(--shc-accent);
}

.shc-root.is-dragging .shc-handle {
  cursor: grabbing;
  transition: none;
}

.shc-handle-icon {
  width: 18px;
  height: 18px;
  fill: none;
  stroke: var(--shc-text-2);
  stroke-width: 1.8;
  stroke-linecap: round;
  stroke-linejoin: round;
}

.shc-track.success .shc-handle {
  cursor: default;
  box-shadow: 0 2px 10px rgba(22, 163, 74, 0.28), inset 0 0 0 1px rgba(22, 163, 74, 0.3);
}

.shc-track.success .shc-handle-icon {
  stroke: var(--shc-success);
}

.shc-root.is-fail .shc-track {
  animation: shc-shake 0.42s cubic-bezier(0.36, 0.07, 0.19, 0.97);
}

@keyframes shc-spin {
  to {
    transform: rotate(360deg);
  }
}

@keyframes shc-draw {
  to {
    stroke-dashoffset: 0;
  }
}

@keyframes shc-shake {
  10%,
  90% {
    transform: translateX(-2px);
  }
  20%,
  80% {
    transform: translateX(3px);
  }
  30%,
  50%,
  70% {
    transform: translateX(-5px);
  }
  40%,
  60% {
    transform: translateX(5px);
  }
}

@media (prefers-reduced-motion: reduce) {
  .shc-root,
  .shc-handle,
  .shc-piece,
  .shc-track-fill,
  .shc-check svg,
  .shc-refresh.spinning svg {
    animation: none !important;
    transition: none !important;
  }
}
</style>
