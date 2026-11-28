# 山河验证码 · Vue 3 组件

滑块交互和轨迹采集组件，验证结果由后端判断。Vue 只作为 peerDependency，不打进包。

## 安装

```bash
npm i shanhe-captcha-vue
```

仓库内调试：

```bash
npm i ../shanhe-captcha/frontend
```

## 使用

```ts
import { createApp } from 'vue'
import ShanheCaptchaPlugin from 'shanhe-captcha-vue'
import 'shanhe-captcha-vue/style.css'

createApp(App).use(ShanheCaptchaPlugin).mount('#app')
```

插件只是全局注册 `ShanheCaptcha`；不想全局注册就直接 import 组件：

```vue
<template>
  <ShanheCaptcha
    api-base="/captcha"
    theme="auto"
    @success="onPass"
    @fail="(code) => console.log('失败', code)"
  />
</template>

<script setup lang="ts">
import { ShanheCaptcha, captchaClientHeader } from 'shanhe-captcha-vue'

function onPass(ticket: string) {
  fetch('/api/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...captchaClientHeader() },
    body: JSON.stringify({ ticket }),
  })
}
</script>
```

PoW 由组件自动求解（纯 JS sha256，难度由服务端下发），第二道校验也自动处理，业务侧只等 `success`。

## 属性

| 属性 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `api-base` | string | `/captcha` | 后端挂载路径 |
| `width` | number | 344 | 组件总宽，图片区域约 320 |
| `theme` | `light \| dark \| auto` | `light` | auto 跟随系统 |
| `disabled` | boolean | false | 禁用 |
| `auto-load` | boolean | true | 挂载后自动拉挑战 |
| `texts` | object | 内置中文 | 逐条覆盖文案 |
| `headers` | HeadersInit | — | 额外请求头 |
| `request-init` | RequestInit | — | 传给 fetch 的配置 |

事件：

- `success(ticket)` 拿到一次性票据
- `fail(code)` 服务端拒绝，附失败码
- `stage('puzzle' | 'plain')` 第二道校验切换
- `refresh` 换了一张
- `error(err)` 网络或请求异常（429 时组件显示「操作过于频繁」）

实例方法：`refresh()`、`reset()`、`getTicket()`。

## 文案

`texts` 按 key 覆盖，全部 key：

```ts
{
  title: '安全验证',
  hint: '向右拖动滑块完成拼图',
  decoyHint: '多个缺口，按背景边缘选择',
  plainHeader: '最后一步',
  plainHint: '按住滑块，拖到最右侧',
  plainTitle: '最后一步：拖到最右侧',
  success: '验证通过',
  fail: '验证失败，请重试',
  loading: '加载中',
  refresh: '换一张',
  networkError: '网络异常，请重试',
  tooManyRequests: '操作过于频繁，稍后再试',
}
```

## 主题

组件用 CSS 变量配色，在外层覆盖即可：

```css
.my-page .shc-root {
  --shc-accent: #7c3aed;
  --shc-accent-soft: rgba(124, 58, 237, 0.16);
  --shc-bg: #ffffff;
  --shc-text: #1f2733;
  --shc-text-2: #69727f;
  --shc-track: #f1f4f8;
  --shc-hint: #9aa5b3;
  --shc-border: rgba(15, 23, 42, 0.08);
  --shc-success: #16a34a;
}
```

`theme="dark"` 时组件自带一套暗色值，同样可以覆盖。

## 命名与样式隔离

- 类名统一 `shc-` 前缀，动画名 `shc-spin` 等，全部选择器限定在组件根节点 `.shc-root` 内，不会命中宿主页面的同名 class。
- 设备标识存 localStorage 键 `shanhe-captcha-client-id`，请求头 `X-Captcha-Client`。
- 全局注册的组件名是 `ShanheCaptcha`；直接 import 时叫什么由你定。

## 不用组件，只用请求逻辑

```ts
import { fetchChallenge, verifyChallenge, captchaClientHeader } from 'shanhe-captcha-vue'

const ch = await fetchChallenge({ apiBase: '/captcha' })
const res = await verifyChallenge({ id: ch.id, x: offset, trail, behavior })

if (res.next === 'plain') {
  const second = await verifyChallenge({
    id: res.nextId!,
    x: 100,
    trail: secondTrail,
  })
}
```

`fetchChallenge` / `verifyChallenge` 自动带 `X-Captcha-Client`；业务请求用 `captchaClientHeader()` 带上同一个标识。

## 示例页

```bash
cd frontend
npm install
npm run build:demo
```

构建结果在 `frontend/demo-dist`，Go 后端的 `example` 读取这个目录。示例页覆盖拖动、校验、票据和登录消费的完整流程。展示站（链路观察、轨迹回放、SSE 事件流、内嵌接入文档）在独立项目 shanhe-captcha-demo 维护，不进本仓库，也不进 npm 包。

开发模式：

```bash
npm run dev  # 5173，代理 /captcha 和 /api 到 127.0.0.1:8090
```

## 构建

```bash
npm run typecheck
npm run build
npm run build:demo
```

## 发布

```bash
cd frontend
npm pack
npm publish --access public
```

## 协议

MIT