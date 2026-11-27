import type { App } from 'vue'
import ShanheCaptcha from './components/ShanheCaptcha.vue'
import { CaptchaError, captchaClientHeader, fetchChallenge, verifyChallenge } from './client'
import { solvePoW } from './sha256'
import { defaultTexts } from './types'

export { ShanheCaptcha, fetchChallenge, verifyChallenge, CaptchaError, captchaClientHeader, solvePoW, defaultTexts }

export type {
  BehaviorSignals,
  CaptchaClientOptions,
  ChallengeMode,
  CaptchaTexts,
  Challenge,
  ShanheCaptchaEmits,
  ShanheCaptchaExpose,
  ShanheCaptchaProps,
  PoWQuestion,
  TrailPoint,
  VerifyPayload,
  VerifyResponse,
} from './types'

const plugin = {
  install(app: App) {
    app.component('ShanheCaptcha', ShanheCaptcha)
  },
}

export default plugin
