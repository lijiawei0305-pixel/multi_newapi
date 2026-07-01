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
import { useEffect, useRef } from 'react'
import { AnimatePresence, motion, useReducedMotion } from 'motion/react'
import { Lock } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import type { CustomDomainStatus } from './types'

// ============================================================================
// CertificateSeal — the animated centerpiece of the custom-domain issuance
// flow. A single self-contained SSL emblem that morphs across the state
// machine (pending_dns → verifying → dns_verified → active | failed) without
// remounting, so `motion` hands off smoothly as the 5s poll advances status.
//
// Design system fidelity: one color hand-off per state via TONE → the whole
// emblem paints with `currentColor`. Progress reads on FOUR independent
// channels (tone + arc length + orbit speed + pulse depth), two of them
// static — so `verifying` vs `dns_verified` stay distinguishable even when
// frozen under prefers-reduced-motion (where the orbit/pulse are suppressed).
// ============================================================================

// Shared premium ease (Vercel/Linear grade). Typed as a 4-tuple so `motion`
// accepts it as a cubic-bezier easing value.
const EASE: [number, number, number, number] = [0.16, 1, 0.3, 1]

// TONE sets `currentColor` for the emblem; ring, bead, shield and plate all
// inherit it. `--primary` is intentional monochrome ink for the star state.
const TONE: Record<CustomDomainStatus, string> = {
  pending_dns: 'text-muted-foreground',
  verifying: 'text-info',
  dns_verified: 'text-primary',
  active: 'text-success',
  failed: 'text-destructive',
}

// Determinate arc fraction per state. Keeps `verifying` vs `dns_verified`
// legible by arc length alone, including the reduced-motion (no-orbit) path.
const FRACTION: Record<CustomDomainStatus, number> = {
  pending_dns: 0.08,
  verifying: 0.4,
  dns_verified: 0.72,
  active: 1,
  failed: 0.4,
}

// Caption copy per state (both lines run through i18n `t()`).
const COPY: Record<CustomDomainStatus, { h: string; s: string }> = {
  pending_dns: {
    h: 'Add DNS records',
    s: 'Add the records below, then verify ownership.',
  },
  verifying: { h: 'Verifying ownership', s: 'Checking your TXT record...' },
  dns_verified: {
    h: 'Issuing certificate',
    s: 'Issuing your HTTPS certificate...',
  },
  active: {
    h: 'Live on HTTPS',
    s: 'Your domain is now served over HTTPS.',
  },
  failed: {
    h: 'Issuance failed',
    s: 'We could not finish this step. Check the error below and retry.',
  },
}

const isBusy = (s: CustomDomainStatus) =>
  s === 'verifying' || s === 'dns_verified'

// lucide `shield` body; a separate `shield-check` tick is carved on success.
const SHIELD =
  'M20 13c0 5-3.5 7.5-7.66 8.95a1 1 0 0 1-.67-.01C7.5 20.5 4 18 4 13V6a1 1 0 0 1 1-1c2 0 4.5-1.2 6.24-2.72a1.17 1.17 0 0 1 1.52 0C14.51 3.81 17 5 19 5a1 1 0 0 1 1 1z'

// 8 sparkle directions for the one-shot success burst (monochrome, no gravity).
const SPARKS = [0, 1, 2, 3, 4, 5, 6, 7]

export function CertificateSeal({
  status,
  lastError,
}: {
  status: CustomDomainStatus
  lastError?: string
}) {
  const { t } = useTranslation()
  const reduce = useReducedMotion()
  const busy = isBusy(status)
  const active = status === 'active'
  const failed = status === 'failed'
  const copy = COPY[status]

  // Freeze the arc at the last non-failed progress rather than snapping back.
  const lastFrac = useRef(FRACTION[status])
  useEffect(() => {
    if (status !== 'failed') lastFrac.current = FRACTION[status]
  }, [status])
  const frac = failed ? lastFrac.current : FRACTION[status]

  // Issuing orbits faster + pulses deeper than verifying = visible escalation.
  const spin = status === 'dns_verified' ? 1.2 : 2
  const pulse = status === 'dns_verified' ? 1.05 : 1.03

  return (
    <div className='flex flex-col items-center gap-4 text-center'>
      <div className={cn('relative grid size-32 place-items-center', TONE[status])}>
        {/* plate */}
        <div
          className={cn(
            'absolute inset-2 rounded-full border border-current/10',
            active ? 'bg-success/10' : 'bg-current/5'
          )}
        />

        {/* ring: static track + determinate progress arc */}
        <svg
          viewBox='0 0 100 100'
          className='absolute inset-0 size-full -rotate-90'
        >
          <circle
            cx='50'
            cy='50'
            r='44'
            fill='none'
            stroke='currentColor'
            strokeWidth='3'
            opacity={0.12}
          />
          <motion.circle
            cx='50'
            cy='50'
            r='44'
            fill='none'
            stroke='currentColor'
            strokeWidth='3'
            strokeLinecap='round'
            opacity={0.9}
            initial={false}
            animate={{ pathLength: frac }}
            transition={
              reduce
                ? { duration: 0 }
                : { type: 'spring', stiffness: 120, damping: 22, mass: 0.9 }
            }
          />
        </svg>

        {/* orbiting bead — the only "working" spin; hidden when idle/reduced */}
        {busy && !reduce && (
          <motion.svg
            viewBox='0 0 100 100'
            className='absolute inset-0 size-full'
            animate={{ rotate: 360 }}
            transition={{ duration: spin, ease: 'linear', repeat: Infinity }}
          >
            <circle cx='50' cy='6' r='2.5' fill='currentColor' />
          </motion.svg>
        )}

        {/* shield body — persists across states; pulses busy, stamps on active,
            flinches once on fail */}
        <motion.svg
          viewBox='0 0 24 24'
          className='relative size-14'
          fill='none'
          stroke='currentColor'
          strokeWidth={1.75}
          strokeLinecap='round'
          strokeLinejoin='round'
          initial={false}
          animate={
            reduce
              ? { scale: 1, x: 0 }
              : active
                ? { scale: [1, 1.06, 1] }
                : failed
                  ? { x: [0, -4, 4, -3, 3, 0] }
                  : busy
                    ? { scale: [1, pulse, 1] }
                    : { scale: 1 }
          }
          transition={
            reduce
              ? { duration: 0 }
              : active
                ? { duration: 0.52, ease: EASE, times: [0, 0.6, 1] }
                : failed
                  ? { duration: 0.38, ease: 'easeOut' }
                  : busy
                    ? { duration: spin, ease: 'easeInOut', repeat: Infinity }
                    : { duration: 0 }
          }
        >
          <path d={SHIELD} />
          {active && (
            <motion.path
              d='m9 12 2 2 4-4'
              initial={{ pathLength: reduce ? 1 : 0 }}
              animate={{ pathLength: 1 }}
              transition={
                reduce
                  ? { duration: 0 }
                  : { duration: 0.42, delay: 0.2, ease: [0.65, 0, 0.35, 1] }
              }
            />
          )}
        </motion.svg>

        {/* one-shot success FX — single shockwave + monochrome sparkle */}
        {active && !reduce && (
          <>
            <motion.span
              className='pointer-events-none absolute inset-2 rounded-full ring-2 ring-success'
              initial={{ scale: 0.8, opacity: 0.5 }}
              animate={{ scale: 1.8, opacity: 0 }}
              transition={{ duration: 0.7, ease: 'easeOut' }}
            />
            {SPARKS.map((i) => (
              <motion.span
                key={i}
                className='pointer-events-none absolute size-1 rounded-full bg-success'
                initial={{ x: 0, y: 0, scale: 0.5, opacity: 0 }}
                animate={{
                  x: Math.cos((i * Math.PI) / 4) * 46,
                  y: Math.sin((i * Math.PI) / 4) * 46,
                  scale: [0.5, 1, 0.4],
                  opacity: [0, 1, 0],
                }}
                transition={{
                  duration: 0.62,
                  delay: 0.2 + i * 0.012,
                  ease: EASE,
                }}
              />
            ))}
          </>
        )}
      </div>

      {/* caption — real English in a live region so screen readers announce it */}
      <div
        role='status'
        aria-live='polite'
        className='flex min-h-[3.25rem] flex-col items-center gap-1'
      >
        <AnimatePresence mode='wait'>
          <motion.div
            key={status}
            initial={reduce ? { opacity: 0 } : { opacity: 0, y: 6 }}
            animate={{ opacity: 1, y: 0 }}
            exit={reduce ? { opacity: 0 } : { opacity: 0, y: -6 }}
            transition={{ duration: reduce ? 0.15 : 0.22, ease: EASE }}
          >
            <h3 className='text-foreground text-base font-semibold'>
              {t(copy.h)}
            </h3>
            <p className='text-muted-foreground text-sm'>
              {failed && lastError ? lastError : t(copy.s)}
            </p>
          </motion.div>
        </AnimatePresence>
      </div>

      {/* quiet trust line — active only */}
      {active && (
        <span className='text-muted-foreground inline-flex items-center gap-1.5 text-xs'>
          <Lock className='size-3' />
          {t("Secured by Let's Encrypt")}
        </span>
      )}
    </div>
  )
}
