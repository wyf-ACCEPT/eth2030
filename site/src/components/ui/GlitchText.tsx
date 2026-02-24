'use client';
import { useReducedMotion } from '@/hooks/useReducedMotion';

interface GlitchTextProps {
  text: string;
  className?: string;
  as?: 'h1' | 'h2' | 'h3' | 'span';
}

export default function GlitchText({ text, className = '', as: Tag = 'h1' }: GlitchTextProps) {
  const reducedMotion = useReducedMotion();

  if (reducedMotion) {
    return <Tag className={`neon-purple ${className}`}>{text}</Tag>;
  }

  return (
    <Tag className={`glitch ${className}`} data-text={text} aria-label={text}>
      {text}
    </Tag>
  );
}
