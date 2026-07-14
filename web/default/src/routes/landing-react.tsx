/* WeDream 落地页 React 概念验证路由（/landing-react）。独立公开路由，不套鉴权布局。 */
import { createFileRoute } from '@tanstack/react-router'

import { LandingReact } from '@/features/landing-react'

export const Route = createFileRoute('/landing-react')({
  component: LandingReact,
})
