'use client';
import AnimatedCounter from '@/components/ui/AnimatedCounter';
import { stats } from '@/lib/data';

export default function StatsBar() {
  return (
    <section className="relative py-16 border-y border-eth-purple/10 bg-eth-surface/50">
      <div className="max-w-7xl mx-auto px-4">
        <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-6 gap-4">
          {stats.map((stat) => (
            <AnimatedCounter
              key={stat.label}
              value={stat.value}
              prefix={stat.prefix}
              suffix={stat.suffix}
              label={stat.label}
            />
          ))}
        </div>
      </div>
    </section>
  );
}
