'use client';
import { motion } from 'framer-motion';
import SectionHeading from '@/components/ui/SectionHeading';
import { roadmapPhases } from '@/lib/data';
import { useInView } from '@/hooks/useInView';

const phaseColors = [
  { border: 'border-eth-purple/40', bg: 'bg-eth-purple/10', dot: 'bg-eth-purple', text: 'text-eth-purple' },
  { border: 'border-eth-teal/40', bg: 'bg-eth-teal/10', dot: 'bg-eth-teal', text: 'text-eth-teal' },
  { border: 'border-eth-blue/40', bg: 'bg-eth-blue/10', dot: 'bg-eth-blue', text: 'text-eth-blue' },
  { border: 'border-eth-pink/40', bg: 'bg-eth-pink/10', dot: 'bg-eth-pink', text: 'text-eth-pink' },
  { border: 'border-eth-purple/40', bg: 'bg-eth-purple/10', dot: 'bg-eth-purple', text: 'text-eth-purple' },
  { border: 'border-eth-teal/40', bg: 'bg-eth-teal/10', dot: 'bg-eth-teal', text: 'text-eth-teal' },
  { border: 'border-eth-blue/40', bg: 'bg-eth-blue/10', dot: 'bg-eth-blue', text: 'text-eth-blue' },
  { border: 'border-eth-pink/40', bg: 'bg-eth-pink/10', dot: 'bg-eth-pink', text: 'text-eth-pink' },
];

export default function RoadmapTimeline() {
  const { ref, isInView } = useInView();

  return (
    <section id="roadmap" className="py-20 md:py-28 px-4 bg-eth-surface/30">
      <SectionHeading
        title="L1 Strawmap"
        subtitle={
          <>
            Based on the official{' '}
            <a
              href="https://strawmap.org/"
              target="_blank"
              rel="noopener noreferrer"
              className="text-eth-purple hover:underline"
            >
              strawmap.org
            </a>{' '}
            — EF Architecture team&apos;s Ethereum protocol roadmap
          </>
        }
      />

      <div ref={ref} className="max-w-5xl mx-auto">
        {/* Five North Stars */}
        <motion.div
          initial={{ opacity: 0, y: 20 }}
          animate={isInView ? { opacity: 1, y: 0 } : {}}
          transition={{ duration: 0.5 }}
          className="mb-12 flex flex-wrap justify-center gap-3"
        >
          {[
            { label: 'Fast L1', desc: 'Finality in seconds' },
            { label: 'Gigagas L1', desc: '1 Ggas/sec' },
            { label: 'Teragas L2', desc: '1 Gbyte/sec' },
            { label: 'PQ L1', desc: 'Quantum-resistant' },
            { label: 'Private L1', desc: 'Shielded transfers' },
          ].map((star) => (
            <span
              key={star.label}
              className="px-4 py-2 rounded-lg border border-eth-purple/20 bg-eth-purple/5 text-sm"
              title={star.desc}
            >
              <span className="text-eth-purple font-semibold">{star.label}</span>
              <span className="text-eth-dim ml-2 hidden sm:inline">{star.desc}</span>
            </span>
          ))}
        </motion.div>

        {/* Timeline */}
        <div className="relative">
          {/* Vertical line */}
          <div className="absolute left-4 md:left-1/2 md:-translate-x-px top-0 bottom-0 w-0.5 bg-gradient-to-b from-eth-purple/40 via-eth-teal/40 to-eth-blue/40" />

          {roadmapPhases.map((phase, i) => {
            const color = phaseColors[i % phaseColors.length];
            const isLeft = i % 2 === 0;

            return (
              <motion.div
                key={phase.name}
                initial={{ opacity: 0, x: isLeft ? -30 : 30 }}
                animate={isInView ? { opacity: 1, x: 0 } : {}}
                transition={{ duration: 0.5, delay: i * 0.1 }}
                className={`relative flex items-start mb-8 md:mb-10 ${
                  isLeft ? 'md:flex-row' : 'md:flex-row-reverse'
                }`}
              >
                {/* Dot */}
                <div className="absolute left-4 md:left-1/2 -translate-x-1/2 w-3 h-3 rounded-full border-2 border-eth-bg z-10 mt-5">
                  <div className={`w-full h-full rounded-full ${color.dot}`} />
                </div>

                {/* Card */}
                <div
                  className={`ml-10 md:ml-0 md:w-[calc(50%-2rem)] ${
                    isLeft ? 'md:mr-auto md:pr-8' : 'md:ml-auto md:pl-8'
                  }`}
                >
                  <div
                    className={`rounded-xl border ${color.border} ${color.bg} backdrop-blur-sm p-5`}
                  >
                    <div className="flex items-center justify-between mb-3">
                      <h3 className={`text-lg font-bold ${color.text}`}>{phase.name}</h3>
                      <div className="flex items-center gap-2">
                        <span className="text-xs text-eth-dim">{phase.year}</span>
                        <span className="text-xs font-mono px-2 py-0.5 rounded-full bg-eth-teal/10 text-eth-teal border border-eth-teal/20">
                          {phase.coverage}
                        </span>
                      </div>
                    </div>
                    <div className="flex flex-wrap gap-1.5">
                      {phase.highlights.map((item) => (
                        <span
                          key={item}
                          className="text-xs px-2 py-0.5 rounded bg-eth-deep/20 text-eth-dim border border-eth-deep/20"
                        >
                          {item}
                        </span>
                      ))}
                    </div>
                  </div>
                </div>
              </motion.div>
            );
          })}
        </div>

        {/* Summary */}
        <motion.div
          initial={{ opacity: 0, y: 20 }}
          animate={isInView ? { opacity: 1, y: 0 } : {}}
          transition={{ duration: 0.5, delay: 0.9 }}
          className="mt-8 text-center text-sm text-eth-dim"
        >
          <span className="text-eth-teal font-semibold">65/65</span> strawmap items implemented across{' '}
          <span className="text-eth-purple font-semibold">8 upgrade phases</span> &middot;{' '}
          <span className="text-eth-pink font-semibold">30 devnet tests</span> passing
        </motion.div>
      </div>
    </section>
  );
}
