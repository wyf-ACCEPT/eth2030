'use client';
import { motion } from 'framer-motion';
import NeonBorder from '@/components/ui/NeonBorder';
import SectionHeading from '@/components/ui/SectionHeading';
import { features } from '@/lib/data';
import { useInView } from '@/hooks/useInView';

export default function FeaturesGrid() {
  const { ref, isInView } = useInView();

  return (
    <section id="features" className="py-20 md:py-28 px-4">
      <SectionHeading title="Features" subtitle="Comprehensive Ethereum protocol implementation" />

      <div ref={ref} className="max-w-7xl mx-auto grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-6">
        {features.map((feature, i) => (
          <motion.div
            key={feature.title}
            initial={{ opacity: 0, y: 30 }}
            animate={isInView ? { opacity: 1, y: 0 } : {}}
            transition={{ duration: 0.5, delay: i * 0.05 }}
          >
            <NeonBorder color={feature.color} className="h-full p-6 bg-eth-surface/30 backdrop-blur-sm">
              <div className="text-3xl mb-3">{feature.icon}</div>
              <h3 className="text-lg font-bold text-eth-text mb-2">{feature.title}</h3>
              <p className="text-sm text-eth-dim leading-relaxed mb-4">{feature.description}</p>
              <div className="flex flex-wrap gap-2">
                {feature.tags.map((tag) => (
                  <span
                    key={tag}
                    className="text-xs px-2 py-0.5 rounded bg-eth-deep/20 text-eth-dim border border-eth-deep/20"
                  >
                    {tag}
                  </span>
                ))}
              </div>
            </NeonBorder>
          </motion.div>
        ))}
      </div>
    </section>
  );
}
