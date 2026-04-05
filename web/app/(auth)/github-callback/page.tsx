import { OAuthCallbackView } from '@/components/oauth-callback-view'

/** 兼容旧书签；新 OAuth 成功跳转见 /oauth-callback */
export default function GitHubCallbackPage() {
  return <OAuthCallbackView />
}
