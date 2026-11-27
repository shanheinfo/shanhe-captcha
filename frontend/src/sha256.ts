/** 算 PoW 用的 SHA-256，只处理固定长度的消息。 */

const K = new Uint32Array([
  0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
  0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
  0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
  0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
  0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
  0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
  0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
  0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2,
])

const rotr = (x: number, n: number) => ((x >>> n) | (x << (32 - n))) >>> 0

function compress(block: Uint8Array, h: Uint32Array, w: Uint32Array): void {
  for (let i = 0; i < 16; i++) {
    const o = i * 4
    w[i] = ((block[o] << 24) | (block[o + 1] << 16) | (block[o + 2] << 8) | block[o + 3]) >>> 0
  }
  for (let i = 16; i < 64; i++) {
    const x = w[i - 15]
    const y = w[i - 2]
    const s0 = (rotr(x, 7) ^ rotr(x, 18) ^ (x >>> 3)) >>> 0
    const s1 = (rotr(y, 17) ^ rotr(y, 19) ^ (y >>> 10)) >>> 0
    w[i] = (w[i - 16] + s0 + w[i - 7] + s1) >>> 0
  }

  let a = h[0], b = h[1], c = h[2], d = h[3]
  let e = h[4], f = h[5], g = h[6], k = h[7]

  for (let i = 0; i < 64; i++) {
    const S1 = (rotr(e, 6) ^ rotr(e, 11) ^ rotr(e, 25)) >>> 0
    const ch = ((e & f) ^ (~e & g)) >>> 0
    const t1 = (k + S1 + ch + K[i] + w[i]) >>> 0
    const S0 = (rotr(a, 2) ^ rotr(a, 13) ^ rotr(a, 22)) >>> 0
    const maj = ((a & b) ^ (a & c) ^ (b & c)) >>> 0
    const t2 = (S0 + maj) >>> 0

    k = g
    g = f
    f = e
    e = (d + t1) >>> 0
    d = c
    c = b
    b = a
    a = (t1 + t2) >>> 0
  }

  h[0] = (h[0] + a) >>> 0
  h[1] = (h[1] + b) >>> 0
  h[2] = (h[2] + c) >>> 0
  h[3] = (h[3] + d) >>> 0
  h[4] = (h[4] + e) >>> 0
  h[5] = (h[5] + f) >>> 0
  h[6] = (h[6] + g) >>> 0
  h[7] = (h[7] + k) >>> 0
}

const INIT = new Uint32Array([
  0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a,
  0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19,
])

const NONCE_LEN = 6

export class PowSolver {
  private block: Uint8Array
  private h = new Uint32Array(8)
  private w = new Uint32Array(64)
  private prefixLen: number

  constructor(prefix: string) {
    this.prefixLen = prefix.length
    // prefix 加 6 位 nonce 后要能放进一个 block。
    if (this.prefixLen + NONCE_LEN >= 56) {
      throw new Error('pow prefix too long')
    }
    this.block = new Uint8Array(64)
    for (let i = 0; i < this.prefixLen; i++) {
      this.block[i] = prefix.charCodeAt(i) & 0xff
    }
    const total = this.prefixLen + NONCE_LEN
    this.block[total] = 0x80
    const bitLen = total * 8
    this.block[62] = (bitLen >>> 8) & 0xff
    this.block[63] = bitLen & 0xff
  }

  /** 返回满足 bits 个前导零的 nonce，失败返回空串。 */
  async solve(bits: number, deadlineMs = 8000): Promise<string> {
    if (bits <= 0) return ''
    if (bits > 32) bits = 32
    const started = performance.now()
    const max = 1 << 24

    for (let i = 0; i < max; i++) {
      let v = i
      for (let k = NONCE_LEN - 1; k >= 0; k--) {
        const d = v & 0xf
        this.block[this.prefixLen + k] = d < 10 ? 48 + d : 87 + d
        v >>>= 4
      }

      this.h.set(INIT)
      compress(this.block, this.h, this.w)

      if (leadingZeroBits(this.h) >= bits) {
        return toHex(i, NONCE_LEN)
      }

      if ((i & 0x1fff) === 0) {
        await new Promise((resolve) => setTimeout(resolve, 0))
        if (performance.now() - started > deadlineMs) return ''
      }
    }
    return ''
  }
}

function leadingZeroBits(h: Uint32Array): number {
  let zeros = 0
  for (let i = 0; i < 8; i++) {
    const v = h[i]
    if (v === 0) {
      zeros += 32
      continue
    }
    return zeros + Math.clz32(v)
  }
  return zeros
}

function toHex(v: number, len: number): string {
  let s = ''
  for (let k = len - 1; k >= 0; k--) {
    const d = (v >>> (k * 4)) & 0xf
    s += d < 10 ? String(d) : String.fromCharCode(87 + d)
  }
  return s
}

export async function solvePoW(prefix: string, bits: number, deadlineMs = 8000): Promise<string> {
  try {
    return await new PowSolver(prefix).solve(bits, deadlineMs)
  } catch {
    return ''
  }
}
