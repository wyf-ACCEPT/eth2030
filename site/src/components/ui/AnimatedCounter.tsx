'use client';
import { useCountUp } from '@/hooks/useCountUp';
import { useInView } from '@/hooks/useInView';
import { useReducedMotion } from '@/hooks/useReducedMotion';

interface AnimatedCounterProps {
  value: number;
  prefix?: string;
  suffix?: string;
  label: string;
}

export default function AnimatedCounter({ value, prefix = '', suffix = '', label }: AnimatedCounterProps) {
  const { ref, isInView } = useInView();
  const reducedMotion = useReducedMotion();
  const count = useCountUp(value, 1500, isInView && !reducedMotion);

  const displayValue = reducedMotion ? value : count;

  return (
    <div ref={ref} className="text-center px-4 py-6">
      <div className="text-3xl sm:text-4xl md:text-5xl font-bold text-eth-purple neon-purple">
        {prefix}{displayValue.toLocaleString()}{suffix}
      </div>
      <div className="text-sm sm:text-base text-eth-dim mt-2 uppercase tracking-wider">
        {label}
      </div>
    </div>
  );
}
