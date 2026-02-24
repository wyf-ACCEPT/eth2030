'use client';
import Hero from '@/components/Hero';
import StatsBar from '@/components/StatsBar';
import FeaturesGrid from '@/components/FeaturesGrid';
import RoadmapTimeline from '@/components/RoadmapTimeline';
import Architecture from '@/components/Architecture';
import GettingStarted from '@/components/GettingStarted';
import Footer from '@/components/Footer';

export default function Home() {
  return (
    <main className="min-h-screen">
      <Hero />
      <StatsBar />
      <FeaturesGrid />
      <RoadmapTimeline />
      <Architecture />
      <GettingStarted />
      <Footer />
    </main>
  );
}
