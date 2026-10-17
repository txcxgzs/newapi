/*
Copyright (C) 2026 Cuz Technology

This file is part of New API. New API is free software: you can
redistribute it and/or modify it under the terms of the GNU Affero
General Public License as published by the Free Software Foundation,
either version 3 of the License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.
+
+ Adapted from https://github.com/CuzTeam/new-api (AGPL-3.0).
*/
import { AnimatePresence, motion } from 'motion/react'
import { useCallback, useEffect, useState } from 'react'

import { cn } from '@/lib/utils'

interface FlipWordsProps {
  words: string[]
  duration?: number
  className?: string
}

/**
 * Rotating headline words: each word staggers in letter by letter with a
 * blur, then exits with a scale-and-blur flourish.
 */
export function FlipWords({
  words,
  duration = 3000,
  className,
}: FlipWordsProps) {
  const [currentWord, setCurrentWord] = useState(words[0])
  const [isAnimating, setIsAnimating] = useState(false)

  const startAnimation = useCallback(() => {
    setCurrentWord((word) => words[words.indexOf(word) + 1] ?? words[0])
    setIsAnimating(true)
  }, [words])

  useEffect(() => {
    if (isAnimating) {
      return
    }
    const timer = setTimeout(startAnimation, duration)
    return () => clearTimeout(timer)
  }, [isAnimating, duration, startAnimation])

  return (
    <span
      className={cn(
        'relative inline-block min-h-[1.15em] text-left',
        className
      )}
    >
      <AnimatePresence
        mode='wait'
        onExitComplete={() => setIsAnimating(false)}
      >
        <motion.span
          key={currentWord}
          initial={{ opacity: 0, y: 8 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.35, ease: 'easeOut' }}
          exit={{
            opacity: 0,
            y: -8,
          }}
          className='inline-block whitespace-nowrap'
        >
          {currentWord}
        </motion.span>
      </AnimatePresence>
    </span>
  )
}
