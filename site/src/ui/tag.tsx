import type { ReactNode } from 'react'

import { cn } from '@/lib/cn'

/**
 * Muted semantic tones: saturation stays low to fit the palette, but
 * warning/success/danger keep enough hue to signal state at a glance.
 * Pair with `dot` for a status marker.
 */
export type TagTone = 'neutral' | 'brand' | 'success' | 'warning' | 'danger'

const tones: Record<TagTone, string> = {
  neutral: 'bg-white/80 text-ink-2 ring-1 ring-inset ring-line',
  brand: 'bg-brand-soft text-brand-ink',
  success: 'bg-emerald-50 text-emerald-700 ring-1 ring-inset ring-emerald-200/60',
  warning: 'bg-amber-50 text-amber-700 ring-1 ring-inset ring-amber-200/60',
  danger: 'bg-rose-50 text-rose-700 ring-1 ring-inset ring-rose-200/60',
}

export interface TagProps {
  children: ReactNode
  tone?: TagTone
  dot?: boolean
  className?: string
}

export function Tag({ children, tone = 'neutral', dot = false, className }: TagProps) {
  return (
    <span
      className={cn(
        'inline-flex h-[22px] items-center gap-1.5 rounded-full px-2.5 text-caption leading-none',
        tones[tone],
        className,
      )}
    >
      {dot ? <span className="h-1.5 w-1.5 rounded-full bg-current" aria-hidden="true" /> : null}
      {children}
    </span>
  )
}
