'use client';

import { LandingHeader } from './landing-header';
import { LandingHero } from './landing-hero';
import { FeaturesSection } from './features-section';
import { BusinessRolesSection } from './business-roles-section';
import { HowItWorksSection } from './how-it-works-section';
import { OpenSourceSection } from './open-source-section';
import { FAQSection } from './faq-section';
import { LandingFooter } from './landing-footer';

export function GoosarLanding() {
  return (
    <>
      <div className="relative">
        <LandingHeader variant="hero" />
        <LandingHero />
      </div>

      <FeaturesSection />
      <BusinessRolesSection />
      <HowItWorksSection />
      <OpenSourceSection />
      <FAQSection />
      <LandingFooter />
    </>
  );
}
