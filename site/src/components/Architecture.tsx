'use client';
import { motion } from 'framer-motion';
import SectionHeading from '@/components/ui/SectionHeading';
import { architectureLayers } from '@/lib/data';
import { useInView } from '@/hooks/useInView';

const colorMap = {
  purple: {
    border: 'border-eth-purple/30',
    text: 'text-eth-purple',
    bg: 'bg-eth-purple/5',
    glow: 'shadow-[0_0_10px_#8c8dfc22]',
    dot: 'bg-eth-purple',
  },
  teal: {
    border: 'border-eth-teal/30',
    text: 'text-eth-teal',
    bg: 'bg-eth-teal/5',
    glow: 'shadow-[0_0_10px_#2de2e622]',
    dot: 'bg-eth-teal',
  },
  blue: {
    border: 'border-eth-blue/30',
    text: 'text-eth-blue',
    bg: 'bg-eth-blue/5',
    glow: 'shadow-[0_0_10px_#627eea22]',
    dot: 'bg-eth-blue',
  },
} as const;

export default function Architecture() {
  const { ref, isInView } = useInView();

  return (
    <section id="architecture" className="py-20 md:py-28 px-4">
      <SectionHeading
        title="Architecture"
        subtitle="Three layers of the EF Protocol L1 roadmap, fully implemented"
      />

      <div ref={ref} className="max-w-5xl mx-auto space-y-8">
        {architectureLayers.map((layer, i) => {
          const colors = colorMap[layer.color];
          return (
            <motion.div
              key={layer.name}
              initial={{ opacity: 0, y: 30 }}
              animate={isInView ? { opacity: 1, y: 0 } : {}}
              transition={{ duration: 0.5, delay: i * 0.15 }}
              className={`rounded-xl border ${colors.border} ${colors.bg} ${colors.glow} p-6 md:p-8`}
            >
              <h3 className={`text-xl md:text-2xl font-bold ${colors.text} mb-6`}>
                {layer.name}
              </h3>

              <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4">
                {layer.tracks.map((track) => (
                  <div key={track.name} className="space-y-2">
                    <h4 className="text-sm font-semibold text-eth-text uppercase tracking-wider">
                      {track.name}
                    </h4>
                    <ul className="space-y-1">
                      {track.items.map((item) => (
                        <li key={item} className="text-sm text-eth-dim flex items-center gap-2">
                          <span className={`w-1.5 h-1.5 rounded-full ${colors.dot}`} />
                          {item}
                        </li>
                      ))}
                    </ul>
                  </div>
                ))}
              </div>
            </motion.div>
          );
        })}
      </div>
    </section>
  );
}
