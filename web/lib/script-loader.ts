/**
 * 按属性标记一次性加载外部脚本
 *
 * 同一 attr 只插入一个 script 标签：已插入但未加载完时挂载监听等待，
 * 加载完成后以 data-loaded 标记短路，重复调用直接 resolve。
 * 验证码组件（Turnstile / reCAPTCHA / hCaptcha）共用本函数。
 *
 * @param src 脚本地址
 * @param attr 用于在 DOM 中定位该脚本的一次性属性名，如 data-cf-turnstile-api
 * @returns 脚本就绪后 resolve；加载失败时 reject
 */
export function loadScriptOnce(src: string, attr: string): Promise<void> {
  return new Promise((resolve, reject) => {
    const existing = document.querySelector(`script[${attr}]`)
    if (existing) {
      if ((existing as HTMLScriptElement).dataset.loaded === '1') {
        resolve()
        return
      }
      existing.addEventListener('load', () => resolve(), { once: true })
      existing.addEventListener('error', () => reject(new Error('script load failed')), { once: true })
      return
    }
    const s = document.createElement('script')
    s.src = src
    s.async = true
    s.setAttribute(attr, '1')
    s.onload = () => {
      s.dataset.loaded = '1'
      resolve()
    }
    s.onerror = () => reject(new Error('script load failed'))
    document.body.appendChild(s)
  })
}