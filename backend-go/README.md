# 山河验证码 · Go 后端

生成背景图和拼图碎片，保存缺口位置，校验拖动位置、轨迹和行为，通过后签发一次性凭证。只用标准库。

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

`New(Config{})` 也能跑，但校验开关都是关的；从 `DefaultConfig()` 出发改字段，零值字段自动补默认。

### 消费票据

业务接口在执行动作前消费票据，两种写法：

```go
// 中间件：票据放请求头，通过后 info 进 context
mux.Handle("/api/login", c.TicketGuard("X-Captcha-Ticket")(
    http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        info, _ := shanhecaptcha.TicketFromContext(r.Context())
        _ = info
        // 执行业务动作
    }),
))

// 或者在 handler 里手动消费（票据放 body 时）
info, err := c.ConsumeTicketFor(r.Context(), ticket, r)
if err != nil {
    http.Error(w, "captcha required", http.StatusForbidden)
    return
}
```

`ConsumeTicketFor` 核对票据是不是当前客户端拿到的，业务请求要带 `X-Captcha-Client`，Vue 侧用 `captchaClientHeader()` 生成。不需要核对客户端时用 `ConsumeTicket`。

票据一次消费即作废；过期返回 `ErrTicketExpired`，无效或已消费返回 `ErrTicketInvalid`。

## 接口

### `GET {prefix}/challenge`

返回一张挑战图，缺口位置不在响应里。`HEAD` 只返回头，不生成挑战。触发限流返回 429。

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
    { "t": 16, "x": 12.4, "y": 0.3 }
  ],
  "behavior": { "pointerType": "mouse", "events": 42, "trusted": 42, "coalesced": 81 },
  "nonce": "3f9a01"
}
```

`trail` 是相对拖动起点的图片像素坐标，`t` 是毫秒。`nonce` 满足 `sha256(prefix + nonce)` 前导零不少于 `bits` 位。

通过：

```json
{ "ok": true, "ticket": "9f2c…", "score": 88 }
```

开启第二道校验时，第一次通过返回：

```json
{ "ok": true, "next": "plain", "nextId": "…", "score": 88 }
```

前端拿 `nextId` 再提交一次，`id=nextId`、`x=100`，通过后才返回 ticket。

失败码：`invalid`、`expired`、`mismatch`、`too_fast`、`trail`、`pow`、`client_mismatch`、`rate_limited`、`bad_request`、`behavior`。`Debug=true` 时 `reason` 回显失败码，线上保持关闭。

## 配置

`DefaultConfig()` 的默认值：

| 字段 | 默认 | 说明 |
| --- | --- | --- |
| `Prefix` | `/captcha` | 挂载路径，`Mount` 可覆盖 |
| `Width / Height` | 320×160 | 图片尺寸 |
| `PieceSize` | 48 | 碎片基准边长，实际上下浮动 6px |
| `Decoys` | 3 | 假缺口数量，0 关闭，最大 3 |
| `Tolerance` | 4 | 允许的横向误差，像素 |
| `ChallengeTTL` | 90s | 挑战有效期 |
| `TicketTTL` | 60s | 票据有效期 |
| `MinSolveDuration` | 250ms | 签发到提交小于这个时间直接拒绝 |
| `AttemptsPerChallenge` | 1 | 每个挑战可校验次数，失败扣减，扣完作废 |
| `BindToClient` | true | 绑定 IP 段、UA、`X-Captcha-Client` |
| `ClientIPPrefixV4 / V6` | /24、/64 | 绑定和限流的 IP 段大小；移动网络切换多时放宽到 /16、/48 |
| `TrustProxyHeaders` | false | 旧语义：信 XFF 第一个 IP 和 `X-Real-IP`，要求代理覆盖而不是追加 XFF |
| `TrustedProxyCIDRs` | 空 | 可信代理网段，填了以后只信这些网段发来的 XFF，从右往左取第一个非可信 IP；此时 `TrustProxyHeaders` 不生效 |
| `VerifyTrail` | true | 检查轨迹：起点、单调时间、速度上限、终点一致 |
| `MinTrailPoints / MaxTrailPoints` | 8 / 3000 | 轨迹点数量范围 |
| `MaxSpeedPxPerMs` | 10 | 相邻轨迹点最大速度 |
| `Behavior.Enabled` | true | 行为评分 |
| `Behavior.MinScore` | 45 | 分数门槛，0–100 |
| `Behavior.StrictKeyboard` | false | 键盘操作默认不评分，改 true 后也参与 |
| `SecondGate.Enabled` | false | 拼图通过后再加一道无图滑块 |
| `PoW` | 开启，18 bit | 前端先算 nonce 再提交；上限 32 bit |
| `RateLimit` | 8 突发、0.5/s、上限 10000 key | 按 IP 段令牌桶 |
| `Limiter` | 进程内令牌桶 | 自定义限流器，多实例接共享存储 |
| `MaxBodyBytes` | 128KB | verify 请求体上限 |
| `MaxConcurrentGenerations` | CPU 数，最少 4 | 同时生图数量 |
| `Store` | 进程内存 | 挑战与票据存储 |
| `Generator` | 内置生成器 | 图片生成接口 |
| `AllowOrigins` | 空 | 前后端分离时填前端域名；`*` 不带 credentials |
| `AllowHeaders` | Content-Type、X-Captcha-Client、Authorization | 跨域允许的头 |
| `Logger` | slog.Default() | 内部错误日志 |
| `Debug` | false | 失败时回显具体原因 |
| `OnEvent` | nil | 事件回调，见下 |

## 部署形态

### 单实例

默认配置即可。数据在进程内存，重启丢失；`Close()` 只关闭内部创建的存储，外部传入的 `Store` 由调用方自己关。

### 反向代理后面

推荐 `TrustedProxyCIDRs`，只信代理网段发来的 XFF：

```go
cfg := shanhecaptcha.DefaultConfig()
cfg.TrustedProxyCIDRs = []string{"10.0.0.0/8", "127.0.0.1/32"}
```

直连请求（RemoteAddr 不在网段内）忽略 XFF，伪造头无效。网段写错 `New` 直接报错。

nginx 侧正常追加即可：

```nginx
proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
```

### 多实例

所有实例共用存储和限流器。实现 `Store`（建议带 `Take`）和 `RateLimiter`：

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

`TakeStore` 是可选接口，读取和删除一次完成；不实现也能跑，但并发消费同一张票据存在小窗口。`MemoryStore.Get` 返回副本，调用方改返回值不影响存储。

限流器接口：

```go
type RateLimiter interface {
    Allow(key string) bool
}
```

key 带动作前缀：拉挑战 `ch:<IP 段>`，校验 `vf:<IP 段>`。

### HTTPS

传输加密交给部署层，前端代码不做加密。验证码接口和业务接口都走 HTTPS。

## 事件

`OnEvent` 在签发、通过、拒绝、限流时回调，可接日志或监控：

```go
cfg.OnEvent = func(e shanhecaptcha.Event) {
    // e.Kind: challenge / success / failure / rate_limited
    // e.ChallengeID / e.ClientKey / e.Reason / e.Elapsed / e.At
    // e.Notes: 行为评分的扣分明细；e.IP: 限流事件带
}
```

展示项目 shanhe-captcha-demo 的 `/api/events` 就是用这个回调做的 SSE 推送；本仓库的 `example` 只保留登录消费票据的示例。

## 自定义生成器

```go
type Generator interface {
    Generate(ctx context.Context, req GenRequest) (*GenResult, error)
}
```

`GenRequest` 带尺寸、碎片边长、假缺口数和随机源（相同 seed 复现同一张图）。`GenResult` 要求背景已画好缺口、碎片能拼回 `TargetX/TargetY`；坐标只保存在服务端。内置生成器的缺口纹理从远处采样，下发碎片再经过软化，自定义实现建议保持同样的性质。

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

`go test -run TestPieceTemplateMatchRate -v` 看的是下发碎片的模板命中率。

## 已知限制

- `Decoys=3` 时四种方法取一约 10%，本质是在 4 个缺口里猜，靠一次性校验和限流压。
- 轨迹和行为数据由前端提交，服务端做一致性检查。
- 第二道无图滑块没有图片答案，仍依赖位置、轨迹和行为信号。
- 多实例必须共用存储；没有 `TakeStore` 时并发消费存在小窗口。
- 限流器默认进程内，多实例自己实现 `RateLimiter`。

## 协议

MIT