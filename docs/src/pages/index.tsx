import React from 'react';
import Link from '@docusaurus/Link';
import useDocusaurusContext from '@docusaurus/useDocusaurusContext';
import Layout from '@theme/Layout';
import useBaseUrl from '@docusaurus/useBaseUrl';
import { motion } from 'framer-motion';

function HomepageHeader() {
  const logoUrl = useBaseUrl('/img/logo.svg');
  return (
    <header className="hero-centered">
      <div className="container">
        <motion.div
          initial={{ opacity: 0, y: 15 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.6, ease: "easeOut" }}
        >
          <img src={logoUrl} alt="Aeroflare Logo" style={{ width: '80px', marginBottom: '1.5rem' }} />
          <h1 style={{ fontSize: 'clamp(2.2rem, 4.5vw, 3.8rem)', fontWeight: 700, marginBottom: '1rem', letterSpacing: '-0.02em', lineHeight: 1.15 }}>
            High-Performance OCI-Backed <br />
            <span className="gradient-text">Nix Binary Cache</span>
          </h1>
          <p style={{ fontSize: '1.15rem', maxWidth: '650px', margin: '0 auto 2rem', opacity: 0.8, lineHeight: 1.6 }}>
            Bridges the Nix ecosystem and standard container registries to act as a stateless, zero-infrastructure binary substituter.
          </p>
          
          <div style={{ display: 'flex', gap: '0.75rem', justifyContent: 'center', flexWrap: 'wrap', marginBottom: '2.5rem' }}>
            <Link className="md3-btn-primary" to="/docs/tutorials/quick-start">
              Get Started
            </Link>
            <a className="md3-btn-secondary" href="https://github.com/ItzEmoji/aeroflare" target="_blank" rel="noopener noreferrer">
              View on GitHub
            </a>
          </div>

          <motion.div 
            initial={{ opacity: 0, y: 10 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.5, delay: 0.2 }}
            style={{ maxWidth: '550px', margin: '0 auto' }}
          >
            <div className="md3-terminal">
              <div style={{ display: 'flex', gap: '6px', marginBottom: '14px' }}>
                <span style={{ width: '10px', height: '10px', borderRadius: '50%', backgroundColor: '#FF5F56' }}></span>
                <span style={{ width: '10px', height: '10px', borderRadius: '50%', backgroundColor: '#FFBD2E' }}></span>
                <span style={{ width: '10px', height: '10px', borderRadius: '50%', backgroundColor: '#27C93F' }}></span>
              </div>
              <span className="terminal-prompt" style={{ color: 'var(--ifm-color-primary)', fontWeight: 'bold', marginRight: '8px' }}>$</span>
              <span>nix run github:ItzEmoji/aeroflare -- init</span>
            </div>
          </motion.div>
        </motion.div>
      </div>
    </header>
  );
}

function HomepageFeatures() {
  const features = [
    {
      badge: 'Architecture',
      title: 'Stateless Proxying',
      description: 'Retains zero local binary state. Streams .nar blobs directly from OCI.',
    },
    {
      badge: 'Performance',
      title: 'O(1) Manifest Lookups',
      description: 'Tags artifacts directly with the 32-character Nix store path hash, enabling instantaneous lookups.',
    },
    {
      badge: 'Storage',
      title: 'Native OCI Storage',
      description: 'Each package is one OCI image tagged with its store hash — NAR blobs as layers, .narinfo as manifest annotations. No separate metadata store.',
    },
    {
      badge: 'Setup',
      title: 'Interactive Provisioning',
      description: 'Built-in setup wizard for GitHub, GitLab, and Cloudflare Worker deployment.',
    },
  ];

  return (
    <section style={{ padding: '5rem 0', borderTop: '1px solid var(--md-sys-color-outline-variant)' }}>
      <div className="container">
        <h2 style={{ textAlign: 'center', fontSize: '2rem', fontWeight: 600, marginBottom: '3rem' }}>
          Core Capabilities
        </h2>
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(280px, 1fr))', gap: '1.5rem' }}>
          {features.map((feature, i) => (
            <motion.div
              key={i}
              className="md3-card-outlined"
              initial={{ opacity: 0, y: 15 }}
              whileInView={{ opacity: 1, y: 0 }}
              viewport={{ once: true }}
              transition={{ duration: 0.4, delay: i * 0.08 }}
            >
              <div style={{ 
                display: 'inline-block', 
                padding: '4px 10px', 
                borderRadius: '8px', 
                fontSize: '0.75rem', 
                fontWeight: 600, 
                backgroundColor: 'rgba(0, 102, 128, 0.08)',
                color: 'var(--ifm-color-primary)',
                marginBottom: '1rem'
              }}>
                {feature.badge}
              </div>
              <h3 style={{ fontSize: '1.25rem', fontWeight: 600, marginBottom: '0.75rem' }}>
                {feature.title}
              </h3>
              <p style={{ opacity: 0.8, fontSize: '0.925rem', lineHeight: 1.5, margin: 0 }}>
                {feature.description}
              </p>
            </motion.div>
          ))}
        </div>
      </div>
    </section>
  );
}

export default function Home() {
  const {siteConfig} = useDocusaurusContext();
  return (
    <Layout
      title={`${siteConfig.title}`}
      description="The OCI-Nix-Binary-Cache written in Go">
      <HomepageHeader />
      <HomepageFeatures />
    </Layout>
  );
}
