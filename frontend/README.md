# 山河验证码 · Vue 3 组件

这个目录是 Vue 3 组件、演示站和 npm 包。组件负责滑块交互和轨迹采集，验证结果由后端判断。

Vue 只作为外部依赖，不会打包进组件。

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

## 属性

| 属性 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `api-base` | string | `/captcha` | 后端挂载路径 |
| `width` | number | 344 | 组件总宽，图片区域约 320 |
| `theme` | `light \| dark \| auto` | `light` | 主题 |
| `disabled` | boolean | false | 禁用 |
| `auto-load` | boolean | true | 挂载后自动拉取挑战 |
| `texts` | object | 内置中文 | 文案覆盖 |
| `headers` | object | — | 额外请求头 |
| `request-init` | RequestInit | — | 传给 fetch 的配置 |

事件：

- `success(ticket)`
- `fail(code)`
- `stage('puzzle' | 'plain')`
- `refresh`
- `error(err)`

第二道校验由组件自动处理，业务侧不需要判断 `next`。

实例方法：`refresh()`、`reset()`、`getTicket()`。

## 不用组件，只用请求逻辑

```ts
import { fetchChallenge, verifyChallenge } from 'shanhe-captcha-vue'

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

## 演示站

```bash
cd frontend
npm install
npm run build:demo
```

构建结果在 `frontend/demo-dist`，Go 后端的 `example` 会读取这个目录。

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
