'use client';

interface NeonBorderProps {
  children: React.ReactNode;
  color?: 'purple' | 'blue' | 'teal' | 'pink';
  className?: string;
}

const borderColors = {
  purple: 'border-eth-purple/30 hover:border-eth-purple hover:shadow-[0_0_15px_#8c8dfc44]',
  blue: 'border-eth-blue/30 hover:border-eth-blue hover:shadow-[0_0_15px_#627eea44]',
  teal: 'border-eth-teal/30 hover:border-eth-teal hover:shadow-[0_0_15px_#2de2e644]',
  pink: 'border-eth-pink/30 hover:border-eth-pink hover:shadow-[0_0_15px_#ff6b9d44]',
} as const;

export default function NeonBorder({ children, color = 'purple', className = '' }: NeonBorderProps) {
  return (
    <div className={`border rounded-lg transition-all duration-300 ${borderColors[color]} ${className}`}>
      {children}
    </div>
  );
}
