/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import {
  ArrowRight,
  Briefcase,
  Calculator,
  Clock,
  Coins,
  Globe,
  GraduationCap,
  Heart,
  Laptop,
  Megaphone,
  Palette,
  Rocket,
  Share2,
  ShieldCheck,
  TrendingUp,
  Users,
  Zap,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { AnimateInView } from '@/components/animate-in-view'
import { PublicLayout } from '@/components/layout'
import { Footer } from '@/components/layout/components/footer'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { useStatus } from '@/hooks/use-status'

import { getPublicAgentPlans } from './api'
import { HeroIllustration } from './hero-illustration'

/**
 * Public "Agent Program" (代理加盟) landing page.
 *
 * Faithful re-creation of the TokenHub /affiliate reseller landing page,
 * adapted to this project's design system and multi-tenant agent mechanics:
 *
 * - Sections: Hero → 适合人群 → 加入特点 → 三重收益 → 为什么选择 → 如何成为代理
 *   → 代理规则 + 收益计算器 → 终版 CTA.
 * - Brand name is resolved from the live site config (not hard-coded).
 * - The original "成功案例" testimonials were fabricated income claims; per
 *   product decision they are replaced by a factual "三重收益" band describing
 *   the real revenue streams (充值差价 / 消耗分润 / 成本保护).
 * - The commission rules (系统抽成 10%、售价须 ≥ 成本 1.11 倍) are kept and drive
 *   the interactive earnings calculator.
 */

/** 系统抽成比例（占位默认值，与对标页一致；后端固化后可下沉为配置）。 */
const COMMISSION_RATE = 0.1
/** 成本保护线：代理设置的售价必须高于成本的倍数。 */
const MIN_MARKUP = 1.11

/** 代理合作方案卡片的统一展示形状（静态兜底与后端动态代理套餐共用）。 */
type PlanCard = {
  tier: string
  name: string
  desc: string
  currency: string
  anchor: string
  price: string
  period: string
  discount: string
  recommended: boolean
}

function EarningsCalculator() {
  const { t } = useTranslation()
  const [price, setPrice] = useState('100')
  const [cost, setCost] = useState('70')
  const [orders, setOrders] = useState('50')

  const priceNum = Number.parseFloat(price) || 0
  const costNum = Number.parseFloat(cost) || 0
  const ordersNum = Number.parseFloat(orders) || 0

  const { perOrder, monthly, belowFloor, ready } = useMemo(() => {
    const hasInput = priceNum > 0 && costNum > 0
    const below = hasInput && priceNum < costNum * MIN_MARKUP
    const profit = Math.max(0, priceNum - costNum) * (1 - COMMISSION_RATE)
    return {
      perOrder: profit,
      monthly: profit * ordersNum,
      belowFloor: below,
      ready: hasInput && !below,
    }
  }, [priceNum, costNum, ordersNum])

  const fmt = (n: number) =>
    n.toLocaleString('zh-CN', { maximumFractionDigits: 2 })

  const fields = [
    {
      id: 'price',
      label: t('Agent Join Calc Price', { defaultValue: '用户充值售价（元）' }),
      value: price,
      onChange: setPrice,
    },
    {
      id: 'cost',
      label: t('Agent Join Calc Cost', { defaultValue: '您的进货成本（元）' }),
      value: cost,
      onChange: setCost,
    },
    {
      id: 'orders',
      label: t('Agent Join Calc Orders', {
        defaultValue: '预计月销售笔数',
      }),
      value: orders,
      onChange: setOrders,
    },
  ]

  return (
    <Card className='border-primary/20 h-full'>
      <CardHeader>
        <div className='bg-primary/10 text-primary flex h-10 w-10 items-center justify-center rounded-lg'>
          <Calculator className='h-5 w-5' />
        </div>
        <CardTitle className='mt-3'>
          {t('Agent Join Calc Title', { defaultValue: '收益计算器' })}
        </CardTitle>
        <CardDescription>
          {t('Agent Join Calc Desc', {
            defaultValue: '输入售价与成本，实时估算您的分成收益。',
          })}
        </CardDescription>
      </CardHeader>
      <CardContent className='space-y-4'>
        <div className='space-y-3'>
          {fields.map((field) => (
            <div key={field.id} className='space-y-1.5'>
              <label
                htmlFor={field.id}
                className='text-muted-foreground text-xs font-medium'
              >
                {field.label}
              </label>
              <Input
                id={field.id}
                type='number'
                inputMode='decimal'
                min={0}
                className='h-10'
                value={field.value}
                onChange={(e) => field.onChange(e.target.value)}
              />
            </div>
          ))}
        </div>

        {belowFloor ? (
          <p className='text-destructive bg-destructive/10 rounded-lg px-3 py-2 text-xs'>
            {t('Agent Join Calc Floor Warning', {
              defaultValue: '售价需高于成本的 1.11 倍，请调高售价。',
            })}
          </p>
        ) : null}

        <div className='border-border/60 grid grid-cols-2 gap-3 border-t pt-4'>
          <div>
            <p className='text-muted-foreground text-xs'>
              {t('Agent Join Calc Per Order', { defaultValue: '单笔分成' })}
            </p>
            <p className='text-foreground mt-1 text-lg font-semibold'>
              {ready ? `¥${fmt(perOrder)}` : '—'}
            </p>
          </div>
          <div>
            <p className='text-muted-foreground text-xs'>
              {t('Agent Join Calc Monthly', { defaultValue: '预计月收益' })}
            </p>
            <p className='mt-1 bg-gradient-to-r from-blue-500 via-violet-500 to-purple-500 bg-clip-text text-lg font-semibold text-transparent'>
              {ready ? `¥${fmt(monthly)}` : '—'}
            </p>
          </div>
        </div>
        <p className='text-muted-foreground/70 text-[11px] leading-relaxed'>
          {t('Agent Join Calc Formula', {
            defaultValue:
              '公式：（售价 − 成本价）×（1 − 抽成比例），系统抽成 10%。结果仅供估算。',
          })}
        </p>
      </CardContent>
    </Card>
  )
}

/** Section heading: uppercase eyebrow + title, matching the home page style. */
function SectionHeading(props: { eyebrow: string; title: string }) {
  return (
    <AnimateInView className='mb-12 text-center md:mb-16'>
      <p className='text-muted-foreground mb-3 text-xs font-medium tracking-widest uppercase'>
        {props.eyebrow}
      </p>
      <h2 className='text-2xl font-bold tracking-tight md:text-3xl'>
        {props.title}
      </h2>
    </AnimateInView>
  )
}

export function AgentJoin() {
  const { t } = useTranslation()
  const { status } = useStatus()
  const brand =
    (status?.system_name as string | undefined) ||
    t('Agent Join Brand Fallback', { defaultValue: '本平台' })

  // 适合人群
  const audiences = [
    {
      icon: GraduationCap,
      title: t('Agent Join Audience Student Title', { defaultValue: '大学生' }),
      desc: t('Agent Join Audience Student Desc', {
        defaultValue:
          '时间充裕，希望通过推广轻松增加收入，负担部分生活费与娱乐支出。',
      }),
    },
    {
      icon: Megaphone,
      title: t('Agent Join Audience Creator Title', {
        defaultValue: '自媒体从业者',
      }),
      desc: t('Agent Join Audience Creator Desc', {
        defaultValue:
          '拥有一定粉丝基础，只需在文章或帖子末尾附上链接，即可实现额外盈利。',
      }),
    },
    {
      icon: Briefcase,
      title: t('Agent Join Audience Sidejob Title', {
        defaultValue: '兼职或副业',
      }),
      desc: t('Agent Join Audience Sidejob Desc', {
        defaultValue: '无需大量时间投入，只需业余时间简单推广，即可赚取额外收入。',
      }),
    },
    {
      icon: Laptop,
      title: t('Agent Join Audience Freelancer Title', {
        defaultValue: '自由职业者',
      }),
      desc: t('Agent Join Audience Freelancer Desc', {
        defaultValue: '时间灵活，仅通过参与分销活动，即可轻松增加额外收入。',
      }),
    },
  ]

  // 加入特点
  const traits = [
    {
      icon: Share2,
      title: t('Agent Join Trait Channel Title', { defaultValue: '渠道多' }),
      desc: t('Agent Join Trait Channel Desc', {
        defaultValue: '拥有多样化的推广渠道和资源。',
      }),
    },
    {
      icon: Users,
      title: t('Agent Join Trait Network Title', { defaultValue: '人脉广' }),
      desc: t('Agent Join Trait Network Desc', {
        defaultValue: '具有广泛的社交网络和影响力。',
      }),
    },
    {
      icon: Heart,
      title: t('Agent Join Trait Fans Title', { defaultValue: '粉丝基础' }),
      desc: t('Agent Join Trait Fans Desc', {
        defaultValue: '拥有稳定的粉丝群体和关注度。',
      }),
    },
    {
      icon: Clock,
      title: t('Agent Join Trait Time Title', { defaultValue: '空闲多' }),
      desc: t('Agent Join Trait Time Desc', {
        defaultValue: '时间灵活，可以自主安排工作。',
      }),
    },
  ]

  // 三重收益（替代原虚构「成功案例」，改为真实收益来源）
  const revenues = [
    {
      icon: Coins,
      title: t('Agent Join Revenue Markup Title', {
        defaultValue: '充值差价',
      }),
      desc: t('Agent Join Revenue Markup Desc', {
        defaultValue:
          '自定义下级售价，用户充值时的售价与成本之差即时转入您的分成。',
      }),
    },
    {
      icon: TrendingUp,
      title: t('Agent Join Revenue Usage Title', {
        defaultValue: '消耗分润',
      }),
      desc: t('Agent Join Revenue Usage Desc', {
        defaultValue:
          '下级用户每次调用模型产生用量分润，用得越多，您的持续收益越高。',
      }),
    },
    {
      icon: ShieldCheck,
      title: t('Agent Join Revenue Scale Title', {
        defaultValue: '成本保护 · 规模效应',
      }),
      desc: t('Agent Join Revenue Scale Desc', {
        defaultValue:
          '内置成本保护线，杜绝亏本定价；一套上游渠道分发多模型，规模越大成本越低。',
      }),
    },
  ]

  // 为什么选择我们
  const reasons = [
    {
      icon: Zap,
      title: t('Agent Join Reason Easy Title', { defaultValue: '简易设置' }),
      desc: t('Agent Join Reason Easy Desc', {
        defaultValue:
          '只需提供一个域名，无需部署 API、无需寻找支付渠道，免备货、免提现手续费。',
      }),
    },
    {
      icon: Palette,
      title: t('Agent Join Reason Custom Title', {
        defaultValue: '完全定制化',
      }),
      desc: t('Agent Join Reason Custom Desc', {
        defaultValue:
          '可定制价格、首页、教程、Logo、公告等，预设首页随价格与站名变动，也可替换为自己的。',
      }),
    },
    {
      icon: Globe,
      title: t('Agent Join Reason Site Title', { defaultValue: '一键建站' }),
      desc: t('Agent Join Reason Site Desc', {
        defaultValue: '仅需一个域名即可创建同样功能的网站，支持站长部署的全部功能。',
      }),
    },
  ]

  // 如何成为代理（5 步）
  const steps = [
    t('Agent Join Step 1', {
      defaultValue: '申请并获得管理员批准，即可开始配置。',
    }),
    t('Agent Join Step 2', {
      defaultValue: '购买或使用已有域名，并在代理设置中配置。',
    }),
    t('Agent Join Step 3', {
      defaultValue: '在域名服务商添加 CNAME 记录，指向指定地址（联系客服获取）。',
    }),
    t('Agent Join Step 4', {
      defaultValue: '提供域名给管理员以配置 SSL 证书。',
    }),
    t('Agent Join Step 5', {
      defaultValue:
        '配置完成后，访问域名即可看到代理站点，域名下注册的用户将成为您的下级用户。',
    }),
  ]

  // 代理规则
  const rules = [
    t('Agent Join Rule 1', {
      defaultValue: '用户访问您的代理域名进行注册，即与您建立代理关系。',
    }),
    t('Agent Join Rule 2', {
      defaultValue: '用户充值按照您设置的价格进行购买。',
    }),
    t('Agent Join Rule 3', {
      defaultValue: '系统自动扣除成本与抽成，其余转入您的分成金额。',
    }),
    t('Agent Join Rule 4', { defaultValue: '系统抽成 10%。' }),
  ]

  // 代理合作方案 = 代理套餐（一次性 + 有效期）。真实数据来自公开只读端点
  // /api/agent-plans/public（后台可配的三档：普通代理 / OEM 代理 / API 代理）；
  // 拉取失败或后台未配置时回退到这份兜底文案，保证公开落地页永不空白。
  const fallbackPlans: PlanCard[] = [
    {
      tier: '',
      name: t('Agent Join Plan Name Basic', { defaultValue: '普通代理' }),
      desc: t('Agent Join Plan Desc Basic', {
        defaultValue: '适合个人或小团队，快速开始销售 AI 服务',
      }),
      currency: '¥',
      anchor: '1,980',
      price: '990',
      period: t('Agent Join Plan Validity Year', { defaultValue: '有效期 365 天' }),
      discount: t('Agent Join Plan Discount 50', { defaultValue: '5 折优惠' }),
      recommended: true,
    },
    {
      tier: '',
      name: t('Agent Join Plan Name Oem', { defaultValue: 'OEM 代理' }),
      desc: t('Agent Join Plan Desc Oem', {
        defaultValue: '品牌定制、独立域名，搭建专属 AI 平台',
      }),
      currency: '¥',
      anchor: '9,980',
      price: '4,990',
      period: t('Agent Join Plan Validity Year', { defaultValue: '有效期 365 天' }),
      discount: t('Agent Join Plan Discount 50', { defaultValue: '5 折优惠' }),
      recommended: false,
    },
    {
      tier: '',
      name: t('Agent Join Plan Name Api', { defaultValue: 'API 代理' }),
      desc: t('Agent Join Plan Desc Api', {
        defaultValue: '开放接口，为合作方提供 AI 能力',
      }),
      currency: '¥',
      anchor: '19,980',
      price: '9,990',
      period: t('Agent Join Plan Validity Year', { defaultValue: '有效期 365 天' }),
      discount: t('Agent Join Plan Discount 50', { defaultValue: '5 折优惠' }),
      recommended: false,
    },
  ]

  // 拉取主站公开代理套餐（无需登录，仅 enabled 展示字段）；失败或空 → 回退 fallbackPlans。
  const { data: livePlans } = useQuery({
    queryKey: ['agent-join', 'public-agent-plans'],
    queryFn: getPublicAgentPlans,
    staleTime: 5 * 60 * 1000,
  })

  const fmtPrice = (n: number) =>
    Number.isFinite(n) ? n.toLocaleString('zh-CN', { maximumFractionDigits: 2 }) : ''

  const plans: PlanCard[] =
    livePlans && livePlans.length > 0
      ? [...livePlans]
          .sort((a, b) => a.sort - b.sort)
          .map((p) => ({
            tier: p.badge,
            name: p.name,
            desc: p.description,
            currency: '¥',
            anchor:
              p.anchor_price_cny > p.price_cny ? fmtPrice(p.anchor_price_cny) : '',
            price: fmtPrice(p.price_cny),
            period:
              p.valid_days > 0 ? `有效期 ${p.valid_days} 天` : '',
            discount: p.discount_label,
            recommended: p.is_recommended,
          }))
      : fallbackPlans

  return (
    <PublicLayout showMainContainer={false}>
      {/* ===== Hero ===== */}
      <section className='relative z-10 overflow-hidden px-6 pt-24 pb-16 md:pt-32 md:pb-24'>
        {/* Radial gradient background */}
        <div
          aria-hidden
          className='pointer-events-none absolute inset-0 -z-10 opacity-25 dark:opacity-[0.12]'
          style={{
            background: [
              'radial-gradient(ellipse 60% 50% at 20% 20%, oklch(0.72 0.18 250 / 80%) 0%, transparent 70%)',
              'radial-gradient(ellipse 50% 40% at 80% 15%, oklch(0.65 0.15 200 / 60%) 0%, transparent 70%)',
            ].join(', '),
          }}
        />

        <div className='mx-auto grid max-w-6xl grid-cols-1 items-center gap-12 lg:grid-cols-12 lg:gap-8'>
          {/* Left: copy + CTAs */}
          <div className='flex flex-col items-start text-left lg:col-span-6'>
            <div className='mb-5 inline-flex items-center gap-1.5 rounded-full border border-blue-500/20 bg-blue-500/5 px-3 py-1.5 text-[11px] font-medium text-blue-600 shadow-xs dark:border-blue-400/20 dark:bg-blue-400/5 dark:text-blue-400'>
              <span className='relative flex size-1.5'>
                <span className='absolute inline-flex h-full w-full animate-ping rounded-full bg-blue-400 opacity-75' />
                <span className='relative inline-flex size-1.5 rounded-full bg-blue-500 dark:bg-blue-400' />
              </span>
              <span>
                {t('Agent Join Hero Badge', { defaultValue: '代理加盟计划' })}
              </span>
            </div>

            <h1 className='text-[clamp(2rem,4.5vw,3rem)] leading-[1.15] font-bold tracking-tight'>
              {t('Agent Join Hero Title Prefix', { defaultValue: '成为' })}
              {brand}
              {t('Agent Join Hero Title Middle', { defaultValue: '的' })}
              <span className='bg-gradient-to-r from-blue-400 via-violet-400 to-purple-500 bg-clip-text text-transparent'>
                {t('Agent Join Hero Title Highlight', {
                  defaultValue: '专属代理',
                })}
              </span>
            </h1>
            <p className='text-muted-foreground/80 mt-5 max-w-xl text-base leading-relaxed'>
              {t('Agent Join Hero Subtitle', {
                defaultValue:
                  '轻松开启您的商业旅程：只需提供域名、按教程配置，即可定制价格、首页与教程，获得用户充值分成。',
              })}
            </p>

            <div className='mt-8 flex flex-wrap items-center gap-3'>
              <Button
                className='group h-11 rounded-lg px-5 text-sm font-medium'
                render={<Link to='/register' />}
              >
                {t('Agent Join CTA Apply', { defaultValue: '立即申请加盟' })}
                <ArrowRight className='ml-1.5 size-4 transition-transform duration-200 group-hover:translate-x-0.5' />
              </Button>
              <Button
                variant='outline'
                className='border-border/50 hover:border-border hover:bg-muted/50 h-11 rounded-lg px-5 text-sm font-medium'
                render={<a href='#how-to-join' />}
              >
                {t('Agent Join CTA Learn More', { defaultValue: '了解更多' })}
              </Button>
            </div>
          </div>

          {/* Right: original isometric AI-compute illustration */}
          <div className='w-full lg:col-span-6'>
            <HeroIllustration className='mx-auto w-full max-w-lg drop-shadow-xl' />
          </div>
        </div>
      </section>

      {/* ===== 适合人群 ===== */}
      <section className='border-border/40 relative z-10 border-t px-6 py-20 md:py-28'>
        <div className='mx-auto max-w-6xl'>
          <SectionHeading
            eyebrow={t('Agent Join Audience Eyebrow', {
              defaultValue: '适合人群',
            })}
            title={t('Agent Join Audience Title', {
              defaultValue: '为不同群体提供灵活的收入机会',
            })}
          />
          <div className='grid gap-4 sm:grid-cols-2 lg:grid-cols-4'>
            {audiences.map((item, i) => {
              const Icon = item.icon
              return (
                <AnimateInView
                  key={item.title}
                  delay={i * 80}
                  animation='fade-up'
                  className='h-full'
                >
                  <Card className='h-full'>
                    <CardHeader>
                      <div className='bg-primary/10 text-primary flex h-10 w-10 items-center justify-center rounded-lg'>
                        <Icon className='h-5 w-5' />
                      </div>
                      <CardTitle className='mt-3 text-base'>
                        {item.title}
                      </CardTitle>
                      <CardDescription>{item.desc}</CardDescription>
                    </CardHeader>
                  </Card>
                </AnimateInView>
              )
            })}
          </div>
        </div>
      </section>

      {/* ===== 加入特点 ===== */}
      <section className='border-border/40 relative z-10 border-t px-6 py-20 md:py-28'>
        <div className='mx-auto max-w-6xl'>
          <SectionHeading
            eyebrow={t('Agent Join Trait Eyebrow', {
              defaultValue: '加入门槛',
            })}
            title={t('Agent Join Trait Title', {
              defaultValue: '如果你有以下特点，动动手指就能赚钱',
            })}
          />
          <div className='grid gap-4 sm:grid-cols-2 lg:grid-cols-4'>
            {traits.map((item, i) => {
              const Icon = item.icon
              return (
                <AnimateInView
                  key={item.title}
                  delay={i * 80}
                  animation='fade-up'
                  className='flex flex-col items-center text-center'
                >
                  <div className='text-primary border-border/50 bg-muted/30 mb-4 flex size-14 items-center justify-center rounded-2xl border'>
                    <Icon className='size-6' strokeWidth={1.5} />
                  </div>
                  <h3 className='text-base font-semibold'>{item.title}</h3>
                  <p className='text-muted-foreground mt-1.5 max-w-[220px] text-sm leading-relaxed'>
                    {item.desc}
                  </p>
                </AnimateInView>
              )
            })}
          </div>
        </div>
      </section>

      {/* ===== 三重收益（替代虚构成功案例）===== */}
      <section className='border-border/40 relative z-10 border-t px-6 py-20 md:py-28'>
        <div className='mx-auto max-w-6xl'>
          <SectionHeading
            eyebrow={t('Agent Join Revenue Eyebrow', {
              defaultValue: '收益来源',
            })}
            title={t('Agent Join Revenue Title', {
              defaultValue: '三重收益，规模越大越省',
            })}
          />
          <div className='grid gap-4 md:grid-cols-3'>
            {revenues.map((item, i) => {
              const Icon = item.icon
              return (
                <AnimateInView
                  key={item.title}
                  delay={i * 100}
                  animation='fade-up'
                  className='h-full'
                >
                  <Card className='h-full'>
                    <CardHeader>
                      <div className='bg-primary/10 text-primary flex h-11 w-11 items-center justify-center rounded-xl'>
                        <Icon className='h-5 w-5' />
                      </div>
                      <CardTitle className='mt-3'>{item.title}</CardTitle>
                      <CardDescription>{item.desc}</CardDescription>
                    </CardHeader>
                  </Card>
                </AnimateInView>
              )
            })}
          </div>
        </div>
      </section>

      {/* ===== 为什么选择我们 ===== */}
      <section className='border-border/40 relative z-10 border-t px-6 py-20 md:py-28'>
        <div className='mx-auto max-w-6xl'>
          <SectionHeading
            eyebrow={t('Agent Join Reason Eyebrow', {
              defaultValue: '为什么选择我们',
            })}
            title={t('Agent Join Reason Title', {
              defaultValue: '更专业更便捷，为何与我们合作？',
            })}
          />
          <div className='grid gap-4 md:grid-cols-3'>
            {reasons.map((item, i) => {
              const Icon = item.icon
              return (
                <AnimateInView
                  key={item.title}
                  delay={i * 100}
                  animation='fade-up'
                  className='h-full'
                >
                  <Card className='h-full'>
                    <CardHeader>
                      <div className='bg-primary/10 text-primary flex h-11 w-11 items-center justify-center rounded-xl'>
                        <Icon className='h-5 w-5' />
                      </div>
                      <CardTitle className='mt-3'>{item.title}</CardTitle>
                      <CardDescription>{item.desc}</CardDescription>
                    </CardHeader>
                  </Card>
                </AnimateInView>
              )
            })}
          </div>
        </div>
      </section>

      {/* ===== 代理合作方案 ===== */}
      <section className='border-border/40 relative z-10 border-t px-6 py-20 md:py-28'>
        <div className='mx-auto max-w-6xl'>
          <SectionHeading
            eyebrow={t('Agent Join Plans Eyebrow', {
              defaultValue: '代理合作方案',
            })}
            title={t('Agent Join Plans Title', {
              defaultValue: '选择适合你的代理类型，开始销售 AI 服务',
            })}
          />
          <div className='grid gap-4 sm:grid-cols-2 lg:grid-cols-4'>
            {plans.map((plan, i) => (
              <AnimateInView
                key={plan.name}
                delay={i * 80}
                animation='fade-up'
                className='h-full'
              >
                <Card
                  className={
                    plan.recommended
                      ? 'border-primary/40 h-full shadow-lg'
                      : 'h-full'
                  }
                >
                  <CardHeader>
                    {plan.tier || plan.recommended ? (
                      <div className='flex items-center justify-between gap-2'>
                        <span className='text-muted-foreground text-xs'>
                          {plan.tier}
                        </span>
                        {plan.recommended ? (
                          <Badge className='shrink-0'>
                            {t('Agent Join Plan Recommended', {
                              defaultValue: '推荐',
                            })}
                          </Badge>
                        ) : null}
                      </div>
                    ) : null}
                    <CardTitle className='mt-1'>{plan.name}</CardTitle>
                    {plan.desc ? (
                      <CardDescription>{plan.desc}</CardDescription>
                    ) : null}
                  </CardHeader>
                  <CardContent className='space-y-4'>
                    <div>
                      {plan.anchor ? (
                        <p className='text-muted-foreground text-sm line-through'>
                          {plan.currency}
                          {plan.anchor}
                        </p>
                      ) : null}
                      <p className='flex items-baseline gap-1'>
                        <span className='text-3xl font-bold tracking-tight'>
                          {plan.currency}
                          {plan.price}
                        </span>
                      </p>
                      {plan.period ? (
                        <p className='text-muted-foreground mt-1 text-xs'>
                          {plan.period}
                        </p>
                      ) : null}
                      {plan.discount ? (
                        <p className='mt-1 text-xs font-medium text-emerald-600 dark:text-emerald-400'>
                          {plan.discount}
                        </p>
                      ) : null}
                    </div>
                    <Button
                      className='w-full'
                      render={<Link to='/register' />}
                    >
                      {t('Agent Join Plan CTA', { defaultValue: '立即开通' })}
                    </Button>
                  </CardContent>
                </Card>
              </AnimateInView>
            ))}
          </div>
          <p className='text-muted-foreground/70 mt-6 text-center text-xs'>
            {t('Agent Join Plans Footnote', {
              defaultValue: '最终价格与折扣以后台实际配置为准。',
            })}
          </p>
        </div>
      </section>

      {/* ===== 如何成为代理 ===== */}
      <section
        id='how-to-join'
        className='border-border/40 relative z-10 scroll-mt-20 border-t px-6 py-20 md:py-28'
      >
        <div className='mx-auto max-w-3xl'>
          <SectionHeading
            eyebrow={t('Agent Join Steps Eyebrow', {
              defaultValue: '如何成为代理',
            })}
            title={t('Agent Join Steps Title', {
              defaultValue: '申请获批后即可开始配置',
            })}
          />
          <ol className='space-y-6'>
            {steps.map((step, i) => (
              <AnimateInView
                key={step}
                as='li'
                delay={i * 80}
                animation='fade-up'
                className='flex gap-4'
              >
                <div className='bg-foreground text-background flex size-8 shrink-0 items-center justify-center rounded-full text-sm font-bold'>
                  {i + 1}
                </div>
                <p className='text-foreground/90 pt-1 text-sm leading-relaxed md:text-base'>
                  {step}
                </p>
              </AnimateInView>
            ))}
          </ol>
        </div>
      </section>

      {/* ===== 代理规则 + 收益计算器 ===== */}
      <section className='border-border/40 relative z-10 border-t px-6 py-20 md:py-28'>
        <div className='mx-auto max-w-6xl'>
          <SectionHeading
            eyebrow={t('Agent Join Rules Eyebrow', {
              defaultValue: '代理规则',
            })}
            title={t('Agent Join Rules Title', {
              defaultValue: '透明的分成规则，算得清的收益',
            })}
          />
          <div className='grid gap-6 lg:grid-cols-2'>
            <AnimateInView animation='fade-right' className='h-full'>
              <Card className='h-full'>
                <CardHeader>
                  <CardTitle>
                    {t('Agent Join Rules Card Title', {
                      defaultValue: '分成方式 · 直接充值',
                    })}
                  </CardTitle>
                  <CardDescription>
                    {t('Agent Join Rules Card Desc', {
                      defaultValue:
                        '代理可以没有余额；代理设置的价格必须高于成本的 1.11 倍。',
                    })}
                  </CardDescription>
                </CardHeader>
                <CardContent className='space-y-3'>
                  <ul className='space-y-2.5'>
                    {rules.map((rule) => (
                      <li
                        key={rule}
                        className='text-foreground/90 flex gap-2 text-sm leading-relaxed'
                      >
                        <span className='text-primary mt-1.5 size-1.5 shrink-0 rounded-full bg-current' />
                        <span>{rule}</span>
                      </li>
                    ))}
                  </ul>
                  <div className='border-border/60 bg-muted/30 mt-2 rounded-lg border p-3'>
                    <p className='text-muted-foreground text-xs'>
                      {t('Agent Join Rules Formula Label', {
                        defaultValue: '分成计算公式',
                      })}
                    </p>
                    <p className='mt-1 font-mono text-sm'>
                      {t('Agent Join Rules Formula', {
                        defaultValue: '（售价 − 成本价）×（1 − 抽成比例）',
                      })}
                    </p>
                  </div>
                </CardContent>
              </Card>
            </AnimateInView>

            <AnimateInView animation='fade-left' className='h-full'>
              <EarningsCalculator />
            </AnimateInView>
          </div>
        </div>
      </section>

      {/* ===== 终版 CTA ===== */}
      <section className='relative z-10 overflow-hidden px-6 py-24 md:py-32'>
        <div
          aria-hidden
          className='absolute inset-0 -z-10 opacity-20 dark:opacity-[0.08]'
          style={{
            background: [
              'radial-gradient(ellipse 50% 50% at 30% 50%, oklch(0.7 0.15 250 / 70%) 0%, transparent 70%)',
              'radial-gradient(ellipse 40% 40% at 70% 40%, oklch(0.65 0.12 200 / 50%) 0%, transparent 70%)',
            ].join(', '),
          }}
        />
        <AnimateInView className='mx-auto max-w-2xl text-center' animation='scale-in'>
          <h2 className='text-2xl leading-tight font-bold tracking-tight md:text-4xl'>
            {t('Agent Join Final Title', { defaultValue: '准备好开始了吗？' })}
          </h2>
          <p className='text-muted-foreground/80 mx-auto mt-5 max-w-md text-sm leading-relaxed md:text-base'>
            {t('Agent Join Final Subtitle', {
              defaultValue: '申请代理资格，开启您的被动收入之旅。',
            })}
          </p>
          <div className='mt-8 flex items-center justify-center'>
            <Button
              size='lg'
              className='group rounded-lg'
              render={<Link to='/register' />}
            >
              <Rocket className='mr-1.5 size-4' />
              {t('Agent Join Final CTA', { defaultValue: '立即申请成为代理' })}
              <ArrowRight className='ml-1 size-4 transition-transform duration-200 group-hover:translate-x-0.5' />
            </Button>
          </div>
        </AnimateInView>
      </section>

      <Footer />
    </PublicLayout>
  )
}
