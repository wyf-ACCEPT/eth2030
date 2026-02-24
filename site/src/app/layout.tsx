import type { Metadata } from 'next';
import { JetBrains_Mono } from 'next/font/google';
import './globals.css';

const jetbrainsMono = JetBrains_Mono({
  subsets: ['latin'],
  variable: '--font-jetbrains',
  display: 'swap',
});

export const metadata: Metadata = {
  title: 'ETH2030 — Ethereum Client for the 2028 Roadmap',
  description:
    'Experimental Ethereum execution client implementing the EF Protocol L1 Strawmap. 50 packages, 18,000+ tests, 58 EIPs, 100% EF state test conformance. Built in Go.',
  keywords: [
    'Ethereum', 'ETH2030', 'execution client', 'EVM', 'zkVM',
    'PeerDAS', 'SSF', 'post-quantum', 'ePBS', 'native rollups',
  ],
  authors: [{ name: 'ETH2030' }],
  icons: {
    icon: '/favicon.ico',
    apple: '/apple-touch-icon.png',
  },
  openGraph: {
    title: 'ETH2030 — Ethereum Client for the 2028 Roadmap',
    description:
      '50 packages, 18,000+ tests, 58 EIPs, 702K LOC. Full EVM, PQ crypto, native rollups, zkVM, SSF, PeerDAS, ePBS.',
    siteName: 'ETH2030',
    images: [{ url: '/og-image.png', width: 1200, height: 630, alt: 'ETH2030' }],
    locale: 'en_US',
    type: 'website',
  },
  twitter: {
    card: 'summary_large_image',
    title: 'ETH2030 — Ethereum Client for the 2028 Roadmap',
    description: '50 packages, 18,000+ tests, 58 EIPs, 702K LOC.',
    images: ['/og-image.png'],
  },
  robots: { index: true, follow: true },
  metadataBase: new URL('https://eth2030.com'),
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" className={jetbrainsMono.variable}>
      <body className="bg-eth-bg text-eth-text antialiased overflow-x-hidden font-mono">
        {children}
        <div className="scanline-overlay" aria-hidden="true" />
      </body>
    </html>
  );
}
