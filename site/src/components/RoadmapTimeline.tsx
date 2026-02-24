'use client';
import { motion } from 'framer-motion';
import SectionHeading from '@/components/ui/SectionHeading';
import { useInView } from '@/hooks/useInView';

export default function RoadmapTimeline() {
  const { ref, isInView } = useInView();

  return (
    <section id="roadmap" className="py-20 md:py-28 px-4 bg-eth-surface/30">
      <SectionHeading title="Roadmap" subtitle="Multi-phase upgrade plan spanning 2026 to 2030+" />

      <div ref={ref} className="max-w-3xl mx-auto">
        <motion.div
          initial={{ opacity: 0, y: 30 }}
          animate={isInView ? { opacity: 1, y: 0 } : {}}
          transition={{ duration: 0.5 }}
          className="rounded-xl border border-eth-purple/20 bg-eth-bg/80 backdrop-blur-sm p-8 md:p-12 text-center"
        >
          <div className="text-5xl mb-6">&#x1F510;</div>
          <h3 className="text-2xl font-bold text-eth-purple mb-4">Details Coming Soon</h3>
          <p className="text-eth-dim leading-relaxed max-w-lg mx-auto mb-6">
            ETH2030 implements 8 upgrade phases covering consensus, data, and execution layers.
            Full roadmap details will be published when the upstream specification is made public.
          </p>
          <div className="flex flex-wrap justify-center gap-3">
            {['2026', '2027', '2028', '2029', '2030+'].map((year) => (
              <span
                key={year}
                className="px-4 py-1.5 rounded-full border border-eth-purple/30 bg-eth-purple/5 text-eth-purple text-sm"
              >
                {year}
              </span>
            ))}
          </div>
          <div className="mt-8 text-sm text-eth-dim">
            <span className="text-eth-teal font-semibold">65/65</span> roadmap items implemented
          </div>
        </motion.div>
      </div>
    </section>
  );
}
