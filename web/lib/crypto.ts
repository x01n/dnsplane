/** 混合加密不可用（多为 HTTP + 非 localhost，浏览器不提供 crypto.subtle） */
export const ERR_WEB_CRYPTO_UNAVAILABLE = 'WEB_CRYPTO_UNAVAILABLE'

export function isWebCryptoAvailable(): boolean {
  return (
    typeof globalThis !== 'undefined' &&
    typeof globalThis.crypto !== 'undefined' &&
    globalThis.crypto.subtle != null
  )
}

let publicKeyCache: string = ''
let rsaCryptoKey: CryptoKey | null = null
const CUSTOM_BASE62 = '9Kp2LmNqRs4TuVw6XyZ0AaBbCcDdEeFfGgHhIiJj1k3l5MnOoPQr7StUvWxYz8-_'
export interface EncryptedPayload {
  key: string  
  iv: string   
  data: string 
}
// 保留兼容性，但不再作为并发安全机制
let currentAesKey: CryptoKey | null = null
export async function getPublicKey(): Promise<string> {
  if (publicKeyCache) {
    return publicKeyCache
  }
  const response = await fetch('/api/auth/publickey')
  const result = await response.json()
  if (result.code === 0 && result.data?.public_key) {
    publicKeyCache = result.data.public_key
    rsaCryptoKey = null
    return publicKeyCache
  }
  throw new Error('获取公钥失败')
}
export function clearPublicKeyCache() {
  publicKeyCache = ''
  rsaCryptoKey = null
}
async function importPublicKey(pemKey: string): Promise<CryptoKey> {
  if (rsaCryptoKey) return rsaCryptoKey

  const pemContents = pemKey
    .replace(/-----BEGIN PUBLIC KEY-----/, '')
    .replace(/-----END PUBLIC KEY-----/, '')
    .replace(/\s/g, '')

  const binaryString = atob(pemContents)
  const bytes = new Uint8Array(binaryString.length)
  for (let i = 0; i < binaryString.length; i++) {
    bytes[i] = binaryString.charCodeAt(i)
  }

  rsaCryptoKey = await crypto.subtle.importKey(
    'spki',
    bytes.buffer,
    { name: 'RSA-OAEP', hash: 'SHA-256' },
    false,
    ['encrypt']
  )
  return rsaCryptoKey
}

function customEncode(buffer: ArrayBuffer): string {
  const bytes = new Uint8Array(buffer)
  if (bytes.length === 0) return ''

  let result = ''
  for (let i = 0; i < bytes.length; i += 3) {
    const remaining = bytes.length - i
    let chunk: number

    if (remaining >= 3) {
      chunk = (bytes[i] << 16) | (bytes[i + 1] << 8) | bytes[i + 2]
      result += CUSTOM_BASE62[(chunk >> 18) & 0x3F]
      result += CUSTOM_BASE62[(chunk >> 12) & 0x3F]
      result += CUSTOM_BASE62[(chunk >> 6) & 0x3F]
      result += CUSTOM_BASE62[chunk & 0x3F]
    } else if (remaining === 2) {
      chunk = (bytes[i] << 16) | (bytes[i + 1] << 8)
      result += CUSTOM_BASE62[(chunk >> 18) & 0x3F]
      result += CUSTOM_BASE62[(chunk >> 12) & 0x3F]
      result += CUSTOM_BASE62[(chunk >> 6) & 0x3F]
    } else {
      chunk = bytes[i] << 16
      result += CUSTOM_BASE62[(chunk >> 18) & 0x3F]
      result += CUSTOM_BASE62[(chunk >> 12) & 0x3F]
    }
  }
  return result
}

export function customDecode(encoded: string): ArrayBuffer {
  if (encoded.length === 0) return new ArrayBuffer(0)

  const decodeMap: Record<string, number> = {}
  for (let i = 0; i < CUSTOM_BASE62.length; i++) {
    decodeMap[CUSTOM_BASE62[i]] = i
  }

  const result: number[] = []
  for (let i = 0; i < encoded.length; i += 4) {
    const remaining = encoded.length - i

    if (remaining >= 4) {
      const v0 = decodeMap[encoded[i]]
      const v1 = decodeMap[encoded[i + 1]]
      const v2 = decodeMap[encoded[i + 2]]
      const v3 = decodeMap[encoded[i + 3]]
      const chunk = (v0 << 18) | (v1 << 12) | (v2 << 6) | v3
      result.push((chunk >> 16) & 0xFF, (chunk >> 8) & 0xFF, chunk & 0xFF)
    } else if (remaining === 3) {
      const v0 = decodeMap[encoded[i]]
      const v1 = decodeMap[encoded[i + 1]]
      const v2 = decodeMap[encoded[i + 2]]
      const chunk = (v0 << 18) | (v1 << 12) | (v2 << 6)
      result.push((chunk >> 16) & 0xFF, (chunk >> 8) & 0xFF)
    } else if (remaining === 2) {
      const v0 = decodeMap[encoded[i]]
      const v1 = decodeMap[encoded[i + 1]]
      const chunk = (v0 << 18) | (v1 << 12)
      result.push((chunk >> 16) & 0xFF)
    }
  }
  return new Uint8Array(result).buffer
}

// 生成随机nonce
function generateNonce(): string {
  const bytes = new Uint8Array(16)
  crypto.getRandomValues(bytes)
  return Array.from(bytes).map(b => b.toString(16).padStart(2, '0')).join('')
}

// 从rt+at+st派生签名密钥
async function deriveSignKey(): Promise<CryptoKey> {
  const refreshToken = localStorage.getItem('refresh_token') || ''
  const accessToken = localStorage.getItem('token') || ''
  const secretToken = localStorage.getItem('secret_token') || ''
  
  // 如果没有secret_token，生成一个并保存
  let st = secretToken
  if (!st) {
    const bytes = new Uint8Array(32)
    crypto.getRandomValues(bytes)
    st = Array.from(bytes).map(b => b.toString(16).padStart(2, '0')).join('')
    localStorage.setItem('secret_token', st)
  }
  
  // 使用SHA-256(rt+at+st)派生密钥
  const combined = refreshToken + accessToken + st
  const encoder = new TextEncoder()
  const hashBuffer = await crypto.subtle.digest('SHA-256', encoder.encode(combined))
  
  return crypto.subtle.importKey(
    'raw',
    hashBuffer,
    { name: 'HMAC', hash: 'SHA-256' },
    false,
    ['sign']
  )
}

// 获取签名所需的tokens用于请求头
export function getSignTokens(): { refreshToken: string; secretToken: string } {
  return {
    refreshToken: localStorage.getItem('refresh_token') || '',
    secretToken: localStorage.getItem('secret_token') || '',
  }
}

// 过滤掉 undefined/null 值，与 JSON.stringify 行为一致
function cleanObject(obj: Record<string, unknown>): Record<string, unknown> {
  return Object.fromEntries(
    Object.entries(obj).filter(([, v]) => v !== undefined && v !== null)
  )
}

// 将对象按key排序后转为字符串
function sortMapToString(obj: Record<string, unknown>): string {
  const keys = Object.keys(obj).sort()
  const parts: string[] = []
  for (const k of keys) {
    const v = obj[k]
    // 跳过 undefined/null 值（与 JSON.stringify 行为一致）
    if (v === null || v === undefined) continue
    let vStr: string
    if (typeof v === 'string') {
      vStr = v
    } else if (typeof v === 'number') {
      vStr = Number.isInteger(v) ? v.toString() : v.toString()
    } else if (typeof v === 'boolean') {
      vStr = v.toString()
    } else if (typeof v === 'object' && !Array.isArray(v)) {
      vStr = sortMapToString(v as Record<string, unknown>)
    } else if (Array.isArray(v)) {
      // 数组：对每个元素递归排序 key（确保与后端 json.Marshal 的 key 排序一致）
      vStr = JSON.stringify(v, (_key, val) => {
        if (val && typeof val === 'object' && !Array.isArray(val)) {
          // 按 key 排序对象
          const sorted: Record<string, unknown> = {}
          Object.keys(val).sort().forEach(k => { sorted[k] = val[k] })
          return sorted
        }
        return val
      })
    } else {
      vStr = JSON.stringify(v)
    }
    parts.push(`${k}=${vStr}`)
  }
  return parts.join('&')
}

async function generateSign(timestamp: number, nonce: string, data: Record<string, unknown>): Promise<string> {
  const signKey = await deriveSignKey()
  const sortedData = sortMapToString(data)
  const message = `${timestamp}${nonce}${sortedData}`
  const encoder = new TextEncoder()
  const signature = await crypto.subtle.sign('HMAC', signKey, encoder.encode(message))
  return Array.from(new Uint8Array(signature)).map(b => b.toString(16).padStart(2, '0')).join('')
}

export interface HybridEncryptResult {
  payload: EncryptedPayload
  aesKey: CryptoKey
}

export async function hybridEncrypt(data: object | string): Promise<HybridEncryptResult> {
  if (!isWebCryptoAvailable()) {
    throw new Error(ERR_WEB_CRYPTO_UNAVAILABLE)
  }
  const publicKey = await getPublicKey()
  const rsaKey = await importPublicKey(publicKey)
  const aesKey = await crypto.subtle.generateKey(
    { name: 'AES-GCM', length: 256 },
    true,
    ['encrypt', 'decrypt']
  )
  // 保留兼容性赋值（单请求场景仍可用）
  currentAesKey = aesKey
  const iv = crypto.getRandomValues(new Uint8Array(12))
  const encoder = new TextEncoder()
  
  // 添加时间戳、nonce和签名
  const rawData = typeof data === 'string' ? JSON.parse(data) : data
  // 过滤掉 undefined/null 值，确保与 JSON.stringify 行为一致（后端不会收到这些字段）
  const dataObj = cleanObject(rawData as Record<string, unknown>)
  const timestamp = Date.now()
  const nonce = generateNonce()
  const sign = await generateSign(timestamp, nonce, dataObj)
  
  const signedData = {
    ...dataObj,
    _t: timestamp,
    _n: nonce,
    _s: sign,
  }
  
  const dataStr = JSON.stringify(signedData)
  const encryptedData = await crypto.subtle.encrypt(
    { name: 'AES-GCM', iv },
    aesKey,
    encoder.encode(dataStr)
  )
  const rawAesKey = await crypto.subtle.exportKey('raw', aesKey)
  const encryptedKey = await crypto.subtle.encrypt(
    { name: 'RSA-OAEP' },
    rsaKey,
    rawAesKey
  )

  return {
    payload: {
      key: customEncode(encryptedKey),
      iv: customEncode(iv.buffer),
      data: customEncode(encryptedData),
    },
    aesKey,
  }
}

export async function rsaEncrypt(data: string): Promise<string> {
  const publicKey = await getPublicKey()
  const cryptoKey = await importPublicKey(publicKey)
  const encoder = new TextEncoder()
  const dataBytes = encoder.encode(data)
  const encryptedBuffer = await crypto.subtle.encrypt(
    { name: 'RSA-OAEP' },
    cryptoKey,
    dataBytes
  )
  return customEncode(encryptedBuffer)
}
export interface EncryptedResponsePayload {
  iv: string
  data: string
}

export interface EncryptedResponse {
  encrypted: boolean
  payload?: EncryptedResponsePayload
}

// 混淆后的响应格式
export interface ObfuscatedResponse {
  _e: boolean
  _p?: {
    _i: string
    _d: string
  }
}

export async function decryptResponse<T>(response: unknown, aesKey?: CryptoKey): Promise<T> {
  const resp = response as Record<string, unknown>
  // 优先使用传入的 per-request key，回退到全局 currentAesKey
  const key = aesKey || currentAesKey
  
  // 处理混淆格式 (_e, _p._i, _p._d)
  if (resp._e && resp._p) {
    if (!key) {
      console.error('没有可用的AES密钥解密响应')
      throw new Error('解密失败：缺少会话密钥')
    }
    const obfPayload = resp._p as { _i: string; _d: string }
    const iv = new Uint8Array(customDecode(obfPayload._i))
    const encryptedData = customDecode(obfPayload._d)
    const decryptedData = await crypto.subtle.decrypt(
      { name: 'AES-GCM', iv },
      key,
      encryptedData
    )
    const decoder = new TextDecoder()
    const jsonStr = decoder.decode(decryptedData)
    return JSON.parse(jsonStr) as T
  }
  
  // 兼容旧格式 (encrypted, payload.iv, payload.data)
  if (resp.encrypted && resp.payload) {
    if (!key) {
      console.error('没有可用的AES密钥解密响应')
      throw new Error('解密失败：缺少会话密钥')
    }
    const payload = resp.payload as EncryptedResponsePayload
    const iv = new Uint8Array(customDecode(payload.iv))
    const encryptedData = customDecode(payload.data)
    const decryptedData = await crypto.subtle.decrypt(
      { name: 'AES-GCM', iv },
      key,
      encryptedData
    )
    const decoder = new TextDecoder()
    const jsonStr = decoder.decode(decryptedData)
    return JSON.parse(jsonStr) as T
  }
  
  // 未加密的响应直接返回
  return response as T
}
