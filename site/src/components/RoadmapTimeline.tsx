'use client';
import { motion } from 'framer-motion';
import SectionHeading from '@/components/ui/SectionHeading';
import { roadmapPhases } from '@/lib/data';
import { useInView } from '@/hooks/useInView';

export default function RoadmapTimeline() {
  const { ref, isInView } = useInView();

  return (
    <section id="roadmap" className="py-20 md:py-28 px-4 bg-eth-surface/30">
      <SectionHeading title="Roadmap" subtitle="8 upgrade phases from Glamsterdam to the Giga-Gas era" />

      <div ref={ref} className="max-w-6xl mx-auto">
        {/* Desktop: alternating vertical timeline */}
        <div className="hidden md:block relative">
          <div className="absolute left-1/2 top-0 bottom-0 w-px bg-gradient-to-b from-eth-purple via-eth-teal to-eth-blue" />

          {roadmapPhases.map((phase, i) => {
            const isLeft = i % 2 === 0;
            return (
              <motion.div
                key={phase.name}
                initial={{ opacity: 0, x: isLeft ? -30 : 30 }}
                animate={isInView ? { opacity: 1, x: 0 } : {}}
                transition={{ duration: 0.5, delay: i * 0.1 }}
                className={`relative flex items-center mb-12 last:mb-0 ${
                  isLeft ? 'justify-start' : 'justify-end'
                }`}
              >
                <div className="absolute left-1/2 -translate-x-1/2 w-4 h-4 rounded-full bg-eth-purple border-2 border-eth-bg shadow-[0_0_10px_#8c8dfc66] z-10" />

                <div className={`w-5/12 p-5 rounded-lg border border-eth-purple/20 bg-eth-bg/80 backdrop-blur-sm
                  hover:border-eth-purple/50 transition-all duration-300`}>
                  <div className="flex items-baseline justify-between mb-2">
                    <h3 className="text-lg font-bold text-eth-purple">{phase.name}</h3>
                    <span className="text-xs text-eth-dim bg-eth-deep/20 px-2 py-0.5 rounded">
                      {phase.year}
                    </span>
                  </div>
                  <div className="text-sm font-semibold text-eth-teal mb-3">{phase.coverage} coverage</div>
                  <ul className="space-y-1">
                    {phase.highlights.map((h) => (
                      <li key={h} className="text-sm text-eth-dim flex items-start gap-2">
                        <span className="text-eth-purple mt-0.5 text-xs">{'\u25B8'}</span>
                        {h}
                      </li>
                    ))}
                  </ul>
                </div>
              </motion.div>
            );
          })}
        </div>

        {/* Mobile: stacked cards */}
        <div className="md:hidden space-y-6">
          {roadmapPhases.map((phase, i) => (
            <motion.div
              key={phase.name}
              initial={{ opacity: 0, y: 20 }}
              animate={isInView ? { opacity: 1, y: 0 } : {}}
              transition={{ duration: 0.4, delay: i * 0.08 }}
              className="p-5 rounded-lg border border-eth-purple/20 bg-eth-bg/80"
            >
              <div className="flex items-baseline justify-between mb-2">
                <h3 className="text-lg font-bold text-eth-purple">{phase.name}</h3>
                <span className="text-xs text-eth-dim">{phase.year}</span>
              </div>
              <div className="text-sm font-semibold text-eth-teal mb-3">{phase.coverage}</div>
              <div className="flex flex-wrap gap-2">
                {phase.highlights.map((h) => (
                  <span key={h} className="text-xs px-2 py-0.5 rounded bg-eth-deep/20 text-eth-dim">
                    {h}
                  </span>
                ))}
              </div>
            </motion.div>
          ))}
        </div>
      </div>
    </section>
  );
}
