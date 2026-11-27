<script setup lang="ts">
import { ref } from 'vue'
import { ShanheCaptcha, captchaClientHeader } from '../src/index'

const ticket = ref('')
const status = ref<'idle' | 'checking' | 'ok' | 'fail'>('idle')
const message = ref('')
const theme = ref<'light' | 'dark'>('light')
const lastCode = ref('')

function toggleTheme() {
  theme.value = theme.value === 'light' ? 'dark' : 'light'
}

function onSuccess(t: string) {
  ticket.value = t
  lastCode.value = ''
  message.value = '已拿到一次性票据，点「提交」让服务端消费它'
  status.value = 'idle'
}

function onFail(code: string) {
  ticket.value = ''
  lastCode.value = code
  message.value = '服务端判定未通过'
  status.value = 'fail'
}

async function submit() {
  if (!ticket.value) {
    status.value = 'fail'
    message.value = '请先完成滑块验证'
    return
  }
  status.value = 'checking'
  try {
    const resp = await fetch('/api/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', ...captchaClientHeader() },
      body: JSON.stringify({ ticket: ticket.value }),
    })
    const data = await resp.json()
    if (resp.ok && data.ok) {
      status.value = 'ok'
      message.value = data.message
    } else {
      status.value = 'fail'
      message.value = data.message || '票据无效'
    }
  } catch {
    status.value = 'fail'
    message.value = '请求失败，检查后端是否在跑'
  }
}
</script>

<template>
  <div class="page" :class="`page-${theme}`">
    <div class="glow glow-a" />
    <div class="glow glow-b" />

    <main class="shell">
      <section class="copy">
        <span class="badge">山河验证码 · shanhe-captcha</span>
        <h1>滑一下，<br />证明你是人。</h1>
        <p class="lede">
          每张图都是服务端现生成的，答案只存在服务端内存里。走完一次，拿到的是一张绑定设备、
          几十秒过期、只能用一次的票据。
        </p>

        <ul class="points">
          <li><b>一次性挑战</b><span>失败即作废，没有素材库可以被反复标注</span></li>
          <li><b>服务端裁决</b><span>前端拿不到缺口坐标，改 JS 也没用</span></li>
          <li><b>票据绑定</b><span>IP 段 + UA + 设备 ID，不能跨机器转卖</span></li>
        </ul>

        <div class="meta">
          <span>零依赖 Go 包</span>
          <span>Vue 3 组件</span>
          <span>可插拔 Store / 生图器</span>
        </div>
      </section>

      <section class="card">
        <div class="card-head">
          <div>
            <h2>登录</h2>
            <p class="sub">先通过人机验证，再由服务端校验票据</p>
          </div>
          <button class="theme-btn" type="button" @click="toggleTheme">
            {{ theme === 'light' ? '🌙' : '☀️' }}
          </button>
        </div>

        <label class="field">
          <span>账号</span>
          <input value="shanhe@example.com" readonly />
        </label>

        <div class="captcha-block">
          <ShanheCaptcha
            api-base="/captcha"
            :width="344"
            :theme="theme"
            :texts="{ hint: '按住滑块，拖到缺口处', title: '拖动完成拼图' }"
            @success="onSuccess"
            @fail="onFail"
          />
        </div>

        <button class="submit" type="button" :disabled="status === 'checking'" @click="submit">
          <span v-if="status === 'checking'" class="dot-spin" />
          {{ status === 'checking' ? '校验中…' : '提交并登录' }}
        </button>

        <div class="result" :class="`result-${status}`">
          <template v-if="status === 'ok'">
            <span class="tick">✓</span>
            <div>
              <b>{{ message }}</b>
              <p>票据已被服务端消费，第二次提交会失败</p>
            </div>
          </template>
          <template v-else-if="message">
            <span class="dot" />
            <div>
              <b>{{ message }}</b>
              <p v-if="lastCode">失败码：<code>{{ lastCode }}</code></p>
              <p v-else>当前票据：<code>{{ ticket ? ticket.slice(0, 16) + '…' : '（无）' }}</code></p>
            </div>
          </template>
          <template v-else>
            <span class="dot" />
            <div>
              <b>等待操作</b>
              <p>拖动滑块，服务端会校验位置、耗时和轨迹</p>
            </div>
          </template>
        </div>
      </section>
    </main>

    <footer class="foot">
      演示用前端 → <code>GET /captcha/challenge</code> · <code>POST /captcha/verify</code> ·
      <code>POST /api/login</code>
    </footer>
  </div>
</template>

<style>
* {
  box-sizing: border-box;
}

body {
  margin: 0;
  background: #f5f7fb;
}

.page {
  --page-bg: #f5f7fb;
  --page-fg: #0f172a;
  --page-muted: #64748b;
  --card-bg: #ffffff;
  --card-border: rgba(15, 23, 42, 0.08);
  --field-bg: #f8fafc;

  position: relative;
  min-height: 100vh;
  padding: 56px 24px 80px;
  overflow: hidden;
  background: var(--page-bg);
  color: var(--page-fg);
  font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', 'PingFang SC', 'Hiragino Sans GB',
    'Microsoft YaHei', sans-serif;
  transition: background 0.3s ease, color 0.3s ease;
}

.page-dark {
  --page-bg: #0b1017;
  --page-fg: #e8eef6;
  --page-muted: #94a3b8;
  --card-bg: rgba(23, 28, 35, 0.86);
  --card-border: rgba(255, 255, 255, 0.08);
  --field-bg: rgba(255, 255, 255, 0.04);
}

.glow {
  position: absolute;
  width: 520px;
  height: 520px;
  border-radius: 50%;
  filter: blur(90px);
  opacity: 0.55;
  pointer-events: none;
}

.glow-a {
  top: -220px;
  left: -140px;
  background: radial-gradient(circle, rgba(96, 165, 250, 0.55), transparent 65%);
}

.glow-b {
  right: -180px;
  bottom: -240px;
  background: radial-gradient(circle, rgba(168, 85, 247, 0.5), transparent 65%);
}

.shell {
  position: relative;
  z-index: 1;
  display: grid;
  grid-template-columns: minmax(0, 1fr) 392px;
  gap: 72px;
  align-items: center;
  max-width: 1040px;
  margin: 0 auto;
}

.copy .badge {
  display: inline-block;
  padding: 6px 12px;
  border-radius: 999px;
  background: rgba(37, 99, 235, 0.1);
  color: #2563eb;
  font-size: 12px;
  letter-spacing: 0.6px;
}

.page-dark .copy .badge {
  background: rgba(96, 165, 250, 0.14);
  color: #93c5fd;
}

.copy h1 {
  margin: 22px 0 18px;
  font-size: 52px;
  line-height: 1.12;
  letter-spacing: -1.5px;
  font-weight: 700;
}

.copy .lede {
  max-width: 460px;
  margin: 0 0 28px;
  font-size: 15px;
  line-height: 1.75;
  color: var(--page-muted);
}

.points {
  display: grid;
  gap: 14px;
  margin: 0 0 32px;
  padding: 0;
  list-style: none;
}

.points li {
  display: grid;
  grid-template-columns: 96px 1fr;
  gap: 14px;
  align-items: baseline;
  font-size: 13.5px;
  line-height: 1.6;
  color: var(--page-muted);
}

.points b {
  color: var(--page-fg);
  font-weight: 600;
}

.meta {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.meta span {
  padding: 5px 10px;
  border-radius: 8px;
  border: 1px solid var(--card-border);
  font-size: 12px;
  color: var(--page-muted);
}

.card {
  position: relative;
  padding: 26px;
  border-radius: 20px;
  background: var(--card-bg);
  border: 1px solid var(--card-border);
  box-shadow: 0 24px 60px rgba(15, 23, 42, 0.12);
  backdrop-filter: blur(12px);
}

.page-dark .card {
  box-shadow: 0 28px 70px rgba(0, 0, 0, 0.45);
}

.card-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  margin-bottom: 22px;
}

.card-head h2 {
  margin: 0 0 4px;
  font-size: 19px;
  letter-spacing: -0.3px;
}

.card-head .sub {
  margin: 0;
  font-size: 12px;
  color: var(--page-muted);
}

.theme-btn {
  width: 34px;
  height: 34px;
  border-radius: 10px;
  border: 1px solid var(--card-border);
  background: transparent;
  cursor: pointer;
  font-size: 15px;
  line-height: 1;
}

.field {
  display: block;
  margin-bottom: 16px;
}

.field span {
  display: block;
  margin-bottom: 6px;
  font-size: 11.5px;
  letter-spacing: 0.4px;
  color: var(--page-muted);
}

.field input {
  width: 100%;
  height: 40px;
  padding: 0 12px;
  border-radius: 10px;
  border: 1px solid var(--card-border);
  background: var(--field-bg);
  color: var(--page-fg);
  font-size: 13px;
  outline: none;
}

.captcha-block {
  margin: 4px 0 18px;
}

.submit {
  position: relative;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  width: 100%;
  height: 44px;
  border: none;
  border-radius: 11px;
  background: linear-gradient(135deg, #2563eb, #4f46e5);
  color: #fff;
  font-size: 14px;
  font-weight: 600;
  letter-spacing: 0.3px;
  cursor: pointer;
  box-shadow: 0 10px 24px rgba(37, 99, 235, 0.28);
  transition: transform 0.15s ease, box-shadow 0.15s ease, opacity 0.15s ease;
}

.submit:hover:not(:disabled) {
  transform: translateY(-1px);
  box-shadow: 0 14px 28px rgba(37, 99, 235, 0.34);
}

.submit:disabled {
  opacity: 0.7;
  cursor: progress;
}

.dot-spin {
  width: 13px;
  height: 13px;
  border-radius: 50%;
  border: 2px solid rgba(255, 255, 255, 0.4);
  border-top-color: #fff;
  animation: spin 0.7s linear infinite;
}

.result {
  display: flex;
  gap: 10px;
  margin-top: 16px;
  padding: 12px 14px;
  border-radius: 12px;
  background: var(--field-bg);
  border: 1px solid var(--card-border);
  font-size: 12.5px;
  line-height: 1.55;
  min-height: 58px;
  color: var(--page-muted);
}

.result b {
  display: block;
  margin-bottom: 2px;
  color: var(--page-fg);
  font-weight: 600;
}

.result p {
  margin: 0;
}

.result code {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 11.5px;
  padding: 1px 5px;
  border-radius: 5px;
  background: rgba(37, 99, 235, 0.1);
  color: #2563eb;
}

.result-ok {
  border-color: rgba(22, 163, 74, 0.24);
  background: rgba(22, 163, 74, 0.08);
}

.result-ok b {
  color: #16a34a;
}

.result-fail {
  border-color: rgba(239, 68, 68, 0.22);
  background: rgba(239, 68, 68, 0.06);
}

.result-fail b {
  color: #ef4444;
}

.tick {
  display: flex;
  align-items: center;
  justify-content: center;
  flex: none;
  width: 22px;
  height: 22px;
  border-radius: 50%;
  background: #16a34a;
  color: #fff;
  font-size: 12px;
  font-weight: 700;
}

.dot {
  flex: none;
  width: 8px;
  height: 8px;
  margin-top: 5px;
  border-radius: 50%;
  background: #94a3b8;
}

.result-ok .dot {
  background: #16a34a;
}

.foot {
  position: relative;
  z-index: 1;
  margin-top: 56px;
  text-align: center;
  font-size: 12px;
  color: var(--page-muted);
}

.foot code {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 11.5px;
}

@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}

@media (max-width: 900px) {
  .shell {
    grid-template-columns: minmax(0, 1fr);
    gap: 40px;
  }

  .copy h1 {
    font-size: 38px;
  }
}
</style>
