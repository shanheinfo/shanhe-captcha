# 山河验证码 · Go 后端

这个目录是 Go 后端。它生成背景图和拼图碎片，保存缺口位置，校验拖动位置、轨迹和行为，通过后签发一张一次性凭证。

Go 端只用标准库，没有第三方依赖。

## 安装

```bash
go get github.com/shanheinfo/shanhe-captcha/backend-go@v0.1.1
```

## 接进项目

```go
import (
    "net/http"

    shanhecaptcha "github.com/shanheinfo/shanhe-captcha/backend-go"
)

func main() {
    c, err := shanhecaptcha.New(shanhecaptcha.DefaultConfig())
    if err != nil {
        panic(err)
    }
    defer c.Close()

    mux := http.NewServeMux()
    c.Mount(mux, "/captcha")
    // GET  /captcha/challenge
    // POST /captcha/verify

    http.ListenAndServe(":8080", mux)
}
```

业务接口在执行操作前消费凭证：

```go
info, err := c.ConsumeTicketFor(r.Context(), ticket, r)
if err != nil {
    http.Error(w, "captcha required", http.StatusForbidden)
    return
}
_ = info
```

`ConsumeTicketFor` 会检查凭证是不是当前客户端拿到的。业务请求要带 `X-Captcha-Client`，Vue 侧用 `captchaClientHeader()` 生成。

不需要检查客户端时，可以用 `ConsumeTicket`。

## 接口

### `GET {prefix}/challenge`

返回一张挑战图。缺口位置不会返回。

```json
{
  "id": "b55fc28f…",
  "background": "data:image/jpeg;base64,…",
  "piece": "data:image/png;base64,…",
  "width": 320,
  "height": 160,
  "pieceX": 13,
  "pieceY": 68,
  "pieceWidth": 48,
  "pieceHeight": 48,
  "decoys": 3,
  "mode": "puzzle",
  "tolerance": 4,
  "expiresIn": 90,
  "pow": { "algo": "sha256", "prefix": "a1b2c3d4", "bits": 18 }
}
```

### `POST {prefix}/verify`

```json
{
  "id": "b55fc28f…",
  "x": 181.6,
  "trail": [
    { "t": 0, "x": 0, "y": 0 },
    { "t": 16, "x": 12.4, "y": 0 }
  ],
  "behavior": { "pointerType": "mouse", "events": 42, "trusted": 42, "coalesced": 81 },
  "nonce": "3f9a01"
}
```

通过返回：

```json
{ "ok": true, "ticket": "9f2c…", "score": 88 }
```

开启第二道校验时，第一次通过返回：

```json
{ "ok": true, "next": "plain", "nextId": "…", "score": 88 }
```

前端拿 `nextId` 再提交一次，`id=nextId`、`x=100`，通过后才返回 ticket。

失败时返回 `code`：

`invalid`、`expired`、`mismatch`、`too_fast`、`trail`、`pow`、`client_mismatch`、`rate_limited`、`bad_request`、`behavior`。

## 配置

`DefaultConfig()` 的默认值：

| 字段 | 默认 | 说明 |
| --- | --- | --- |
| `Prefix` | `/captcha` | 挂载路径 |
| `Width / Height` | 320×160 | 图片尺寸 |
| `PieceSize` | 48 | 碎片基准边长，实际会上下浮动 6px |
| `Decoys` | 3 | 假缺口数量，0 关闭，最大 3 |
| `Tolerance` | 4 | 允许的横向误差，像素 |
| `ChallengeTTL` | 90s | 挑战有效期 |
| `TicketTTL` | 60s | 凭证有效期 |
| `MinSolveDuration` | 250ms | 小于这个时间直接拒绝 |
| `AttemptsPerChallenge` | 1 | 每个挑战可以校验几次 |
| `BindToClient` | true | 绑定 IP 段、UA、`X-Captcha-Client` |
| `ClientIPPrefixV4 / V6` | /24、/64 | IP 段大小；移动网络切换多时可以放宽到 /16、/48 |
| `VerifyTrail` | true | 检查拖动轨迹 |
| `Behavior.Enabled` | true | 开启行为评分 |
| `Behavior.MinScore` | 45 | 分数低于这个值就拒绝 |
| `Behavior.StrictKeyboard` | false | 键盘操作是否也要评分 |
| `SecondGate.Enabled` | false | 拼图通过后再加一道无图滑块 |
| `MinTrailPoints` | 8 | 轨迹点最少数量 |
| `MaxTrailPoints` | 3000 | 轨迹点最多数量 |
| `MaxSpeedPxPerMs` | 10 | 相邻轨迹点的最大速度 |
| `MaxBodyBytes` | 128KB | 请求体大小上限 |
| `MaxConcurrentGenerations` | 自动，最少 4 | 同时生成图片的数量 |
| `RateLimit` | 8 次突发、0.5/s | 按 IP 段限制请求频率 |
| `Limiter` | 进程内令牌桶 | 自定义限流器，多实例接共享存储 |
| `PoW` | 开启，18 bit | 前端先算一个 nonce 再提交；关掉省客户端 CPU，脚本也不用算 |
| `AllowOrigins` | 空 | 前后端分开时填前端域名 |
| `AllowHeaders` | Content-Type、X-Captcha-Client、Authorization | 跨域请求允许的头 |
| `TrustProxyHeaders` | false | 信任 `X-Forwarded-For` 第一个 IP 和 `X-Real-IP`，要求代理覆盖而不是追加 XFF |
| `TrustedProxyCIDRs` | 空 | 可信代理网段。填了以后只信这些网段发来的 XFF，从右往左取第一个非可信 IP |
| `Debug` | false | 失败时返回具体原因 |

## 更换存储

默认数据放在进程内存里，重启就没了。多实例部署时，实现 `Store`，让所有实例共用同一份存储：

```go
type redisStore struct{ c *redis.Client }

func (s redisStore) Get(ctx context.Context, key string) ([]byte, error) {
    v, err := s.c.Get(ctx, key).Bytes()
    if errors.Is(err, redis.Nil) {
        return nil, shanhecaptcha.ErrStoreNotFound
    }
    return v, err
}

func (s redisStore) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
    return s.c.Set(ctx, key, value, ttl).Err()
}

func (s redisStore) Delete(ctx context.Context, key string) error {
    return s.c.Del(ctx, key).Err()
}

func (s redisStore) Take(ctx context.Context, key string) ([]byte, error) {
    return s.c.GetDel(ctx, key).Bytes()
}
```

`TakeStore` 是可选接口。实现 `Take` 后，读取和删除一次完成；不实现也能跑，但同时消费同一张凭证时会有小概率重复。

## 多实例限流

默认限流在进程内，多副本各算各的。实现 `RateLimiter` 后通过 `Config.Limiter` 传入：

```go
type RateLimiter interface {
    Allow(key string) bool
}
```

key 带动作前缀：拉挑战是 `ch:<IP 段>`，校验是 `vf:<IP 段>`。

## 演示和测试

```bash
go test ./...
go test -race ./...
go vet ./...

go run ./example  # 先在 ../frontend 里 npm run build:demo
```

生成器检查：

```bash
go run ./cmd/preview -n 6 -decoys 3
go run ./cmd/attacksim -n 60 -decoys 3
```

`attacksim` 用四种简单方法在生成器输出上找缺口，命中率：

| 方法 | 无假缺口 | `Decoys=3` |
| --- | --- | --- |
| 最暗窗口 | 7% | 2% |
| 最低方差窗口 | 0% | 0% |
| 邻域差异最大窗口 | 33% | 8% |
| 碎片模板匹配 | 0% | 1% |
| 四种取一 | 36% | 10% |

缺口内部的纹理从远处采样，碎片下发前还会经过 `softenPiece`（亚像素形变、模糊、色偏），两者都不再携带缺口位置的原始像素。`go test -run TestPieceTemplateMatchRate -v` 看的是下发碎片的模板命中率。

## 已知限制

- `Decoys=3` 时四种方法取一约 10%，本质是在 4 个缺口里猜。靠每个挑战只能校验一次和 IP 限流压。
- 轨迹和行为数据都由前端提交，可以伪造。服务端只能做一致性检查。
- 第二道无图滑块没有图片答案，但仍然依赖位置、轨迹和行为信号。
- 多实例必须共用存储；没有实现 `TakeStore` 时，并发消费存在小窗口。
- 限流器默认在进程内，多实例部署要自己实现 `RateLimiter` 接共享存储。

## 协议

MIT
