import AboutSection from "@/components/home/AboutSection";
import BlogSection from "@/components/home/BlogSection";
import HeroSlider from "@/components/home/HeroSlider";

export const dynamic = "force-dynamic";

export default function HomePage() {
  return (
    <main className="min-h-screen">
      <HeroSlider />

      <section id="blog">
        <BlogSection />
      </section>

      <AboutSection />
    </main>
  );
}
