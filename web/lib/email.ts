/** 与后端用户邮箱校验一致的宽松格式（避免与 HTML5 type=email 差异过大） */
const USER_EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/

export function isValidUserEmail(email: string): boolean {
  return USER_EMAIL_RE.test(email.trim())
}
