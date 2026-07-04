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
import { Link } from '@tanstack/react-router'
import {
  ArrowRight,
  Coins,
  Palette,
  ShieldCheck,
  TrendingUp,
  Users,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { PublicLayout } from '@/components/layout'
import { buttonVariants } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'

/**
 * Public "Agent Program" (代理加盟) landing page.
 *
 * Linked from the top navigation. Introduces the reseller/agent program to
 * visitors and routes them to registration. Intentionally content-only for now;
 * richer policy copy and an application form can be layered in later.
 */
export function AgentJoin() {
  const { t } = useTranslation()

  const benefits = [
    {
      icon: Coins,
      title: t('Agent Join Benefit Revenue Title', {
        defaultValue: '充值差价 + 消耗分润',
      }),
      description: t('Agent Join Benefit Revenue Desc', {
        defaultValue:
          '自定义下级用户组倍率，充值差价与用量分润双重收益，钱包实时结算。',
      }),
    },
    {
      icon: Palette,
      title: t('Agent Join Benefit Brand Title', {
        defaultValue: 'OEM 品牌定制',
      }),
      description: t('Agent Join Benefit Brand Desc', {
        defaultValue:
          '独立域名、Logo、主题色与站点文案，打造属于你自己的品牌站点。',
      }),
    },
    {
      icon: Users,
      title: t('Agent Join Benefit Users Title', {
        defaultValue: '下级用户管理',
      }),
      description: t('Agent Join Benefit Users Desc', {
        defaultValue: '推广链接与兑换码获客，统一管理下级用户、额度与订单。',
      }),
    },
    {
      icon: TrendingUp,
      title: t('Agent Join Benefit Scale Title', {
        defaultValue: '开放 API 分销',
      }),
      description: t('Agent Join Benefit Scale Desc', {
        defaultValue: '统一 API 网关，一套上游渠道分发多模型，规模越大成本越低。',
      }),
    },
    {
      icon: ShieldCheck,
      title: t('Agent Join Benefit Safe Title', {
        defaultValue: '成本保护与风控',
      }),
      description: t('Agent Join Benefit Safe Desc', {
        defaultValue: '内置成本保护线与限流风控，避免亏本定价与恶意刷量。',
      }),
    },
  ]

  return (
    <PublicLayout>
      <div className='mx-auto max-w-5xl px-4 py-12 md:py-16'>
        {/* Hero */}
        <div className='space-y-6 text-center'>
          <h1 className='text-3xl font-bold tracking-tight md:text-4xl'>
            {t('Agent Join Hero Title', {
              defaultValue: '成为代理商，共享 AI 分销红利',
            })}
          </h1>
          <p className='text-muted-foreground mx-auto max-w-2xl text-base md:text-lg'>
            {t('Agent Join Hero Subtitle', {
              defaultValue:
                '普通代理、OEM 品牌、开放 API 三种模式任选，零技术门槛接入统一网关，管理自己的下级用户与收益。',
            })}
          </p>
          <div className='flex flex-wrap items-center justify-center gap-3'>
            <Link
              to='/register'
              className={buttonVariants({ variant: 'default', size: 'lg' })}
            >
              {t('Agent Join CTA Apply', { defaultValue: '立即申请加盟' })}
              <ArrowRight className='ml-1 h-4 w-4' />
            </Link>
            <Link
              to='/pricing'
              className={buttonVariants({ variant: 'outline', size: 'lg' })}
            >
              {t('Agent Join CTA Pricing', { defaultValue: '查看模型广场' })}
            </Link>
          </div>
        </div>

        {/* Benefits */}
        <div className='mt-14 grid gap-4 sm:grid-cols-2 lg:grid-cols-3'>
          {benefits.map((benefit) => {
            const Icon = benefit.icon
            return (
              <Card key={benefit.title} className='h-full'>
                <CardHeader>
                  <div className='bg-primary/10 text-primary flex h-10 w-10 items-center justify-center rounded-lg'>
                    <Icon className='h-5 w-5' />
                  </div>
                  <CardTitle className='mt-3'>{benefit.title}</CardTitle>
                  <CardDescription>{benefit.description}</CardDescription>
                </CardHeader>
              </Card>
            )
          })}

          {/* Closing CTA card */}
          <Card className='bg-primary/5 border-primary/20 h-full'>
            <CardHeader>
              <CardTitle className='mt-3'>
                {t('Agent Join Closing Title', {
                  defaultValue: '准备好了吗？',
                })}
              </CardTitle>
              <CardDescription>
                {t('Agent Join Closing Desc', {
                  defaultValue:
                    '注册账号后在控制台申请代理权限，审核通过即可开通品牌站点。',
                })}
              </CardDescription>
            </CardHeader>
            <CardContent>
              <Link
                to='/register'
                className={buttonVariants({ variant: 'default' })}
              >
                {t('Agent Join CTA Apply', { defaultValue: '立即申请加盟' })}
                <ArrowRight className='ml-1 h-4 w-4' />
              </Link>
            </CardContent>
          </Card>
        </div>
      </div>
    </PublicLayout>
  )
}
