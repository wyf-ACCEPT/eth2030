'use client';
import { motion } from 'framer-motion';
import SectionHeading from '@/components/ui/SectionHeading';
import { useInView } from '@/hooks/useInView';

const layers = [
  {
    name: 'Consensus Layer',
    description: 'Finality, attestations, validator management, and cryptographic primitives.',
    color: {
      border: 'border-eth-purple/30',
      text: 'text-eth-purple',
      bg: 'bg-eth-purple/5',
      glow: 'shadow-[0_0_10px_#8c8dfc22]',
    },
  },
  {
    name: 'Data Layer',
    description: 'Data availability, blob management, erasure coding, and sampling.',
    color: {
      border: 'border-eth-teal/30',
      text: 'text-eth-teal',
      bg: 'bg-eth-teal/5',
      glow: 'shadow-[0_0_10px_#2de2e622]',
    },
  },
  {
    name: 'Execution Layer',
    description: 'EVM execution, state management, gas pricing, and transaction processing.',
    color: {
      border: 'border-eth-blue/30',
      text: 'text-eth-blue',
      bg: 'bg-eth-blue/5',
      glow: 'shadow-[0_0_10px_#627eea22]',
    },
  },
] as const;

export default function Architecture() {
  const { ref, isInView } = useInView();

  return (
    <section id="architecture" className="py-20 md:py-28 px-4">
      <SectionHeading
        title="Architecture"
        subtitle="Three-layer design covering the full Ethereum protocol stack"
      />

      <div ref={ref} className="max-w-5xl mx-auto grid grid-cols-1 md:grid-cols-3 gap-6">
        {layers.map((layer, i) => (
          <motion.div
            key={layer.name}
            initial={{ opacity: 0, y: 30 }}
            animate={isInView ? { opacity: 1, y: 0 } : {}}
            transition={{ duration: 0.5, delay: i * 0.15 }}
            className={`rounded-xl border ${layer.color.border} ${layer.color.bg} ${layer.color.glow} p-6 md:p-8 text-center`}
          >
            <h3 className={`text-xl md:text-2xl font-bold ${layer.color.text} mb-4`}>
              {layer.name}
            </h3>
            <p className="text-sm text-eth-dim leading-relaxed">
              {layer.description}
            </p>
          </motion.div>
        ))}
      </div>
    </section>
  );
}
