# 山河验证码

一个滑块验证码。前端是 Vue 3，后端目前是 Go，后面会加 Rust。

服务端生成图片、保存缺口位置，前端只负责拖动和提交轨迹。每个挑战默认只能校验一次，通过后拿到一张一次性凭证（ticket）。

![](docs/sample.png)

## 可以做什么

- 每次请求生成新的背景图和碎片，不用固定图库
- 支持同排假缺口：第一个假缺口优先和真缺口放在同一行，大小接近，颜色取各自位置的背景
- 支持拖动轨迹检查、行为评分、按 IP 限流和可选的第二道无图滑块
- 通过后签发一次性凭证，可以绑定 IP 段、UA 和设备标识
- 前端和后端分开，可以单独部署
- 后端目前有 Go 版本，Rust 版本计划中

## 目录

| 路径 | 内容 |
| --- | --- |
| `frontend/` | Vue 3 组件、演示站和 npm 包 |
| `backend-go/` | Go 后端，独立模块 |
| `backend-rust/` | Rust 后端，计划中 |
| `docs/` | 文档截图 |

## 跑起来

```bash
cd frontend
npm install
npm run build:demo

cd ../backend-go
go run ./example
```

打开 <http://127.0.0.1:8090>。演示站默认开启第二道校验，滑块通过后拿到 ticket，再点「提交并登录」由后端消费。

## 文档

- [Go 后端](backend-go/README.md)
- [Vue 组件](frontend/README.md)

## 协议

MIT
