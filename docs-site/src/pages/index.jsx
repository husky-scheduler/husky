import React, {useState} from 'react';
import Layout from '@theme/Layout';
import Link from '@docusaurus/Link';
import useBaseUrl from '@docusaurus/useBaseUrl';
import {useColorMode} from '@docusaurus/theme-common';
import styles from './index.module.css';

const pillars = [
  {
    title: 'Zero Intrusion',
    description: 'No SDK required. Your code stays untouched. If it runs in a terminal, Husky can orchestrate it.',
  },
  {
    title: 'Self-contained',
    description: 'Single binary. No database, no container runtime, no external dependencies to manage.',
  },
  {
    title: 'Language agnostic',
    description: 'Shell, Python, Go, Node — run any script in any language, exactly as you would locally.',
  },
  {
    title: 'Air-Gapped Ready',
    description: 'Runs fully offline. No cloud phone-home, no telemetry, no surprises.',
  },
];

const steps = [
  {
    num: '1',
    title: 'Install the binary',
    description: 'A single curl command. No package manager required.',
    lang: 'bash',
    code: `curl -sSL https://raw.githubusercontent.com/husky-scheduler/husky/main/install.sh | sh`,
  },
  {
    num: '2',
    title: 'Write a workflow',
    description: 'Drop a husky.yaml in any directory.',
    lang: 'yaml',
    code: `tasks:\n  backup:\n    command: pg_dump -Fc mydb > backup.dump\n    schedule: every:1h\n    retry:\n      attempts: 3\n      backoff: exponential\n\n  upload:\n    command: rclone copy backup.dump r2:backups/\n    after: [backup]\n    notify_on: [failure]`,
  },
  {
    num: '3',
    title: 'Start the daemon',
    description: 'Husky starts up in the background',
    lang: 'bash',
    code: `husky start`,
  },
];

const features = [
  {
    title: 'Human-readable schedules',
    description:
      'Define timing with expressive syntax — every:15m, on:[monday,friday], after:build — instead of opaque cron strings.',
    link: '/docs/configuration',
  },
  {
    title: 'DAG-based pipelines',
    description:
      'Wire tasks with after: dependencies. Husky builds the execution graph, detects cycles at load time, and runs tasks in topological order.',
    link: '/docs/task',
  },
  {
    title: 'Crash recovery & catch-up',
    description:
      'On restart, the daemon reconciles stale PIDs, resolves orphan processes, and optionally runs missed schedules.',
    link: '/docs/crash-recovery',
  },
  {
    title: 'Real-time dashboard',
    description:
      'A React-based web UI ships with the daemon. Monitor task history, logs, and live run status from your browser.',
    link: '/docs/dashboard',
  },
  {
    title: 'Notifications',
    description:
      'Push alerts to Slack, PagerDuty, or custom webhooks when tasks fail, succeed, or miss their SLA window.',
    link: '/docs/notifications',
  },
  {
    title: 'Auth & security',
    description:
      'JWT-based API auth, RBAC roles, TLS, and per-task secret injection keep your automation locked down.',
    link: '/docs/security',
  },
];

const docLinks = [
  {label: 'Getting started', to: '/docs', desc: 'Install and configure Husky'},
  {label: 'Configuration', to: '/docs/configuration', desc: 'Full husky.yaml reference'},
  {label: 'DAG pipelines', to: '/docs/task', desc: 'Tasks, dependencies, and graphs'},
  {label: 'Crash recovery', to: '/docs/crash-recovery', desc: 'Reconciliation and catch-up logic'},
  {label: 'Notifications', to: '/docs/notifications', desc: 'Slack, PagerDuty, webhooks'},
  {label: 'Operations', to: '/docs/operations', desc: 'Packaging, systemd, launchd'},
];

export default function Home() {
  return (
    <Layout
      title="Workflows belong in your codebase"
      description="Husky is a project-scoped, Git-versioned workflow daemon. Define jobs in husky.yaml, keep them next to your app, and review, version, and ship them with the rest of your code."
    >
      <HomeContent />
    </Layout>
  );
}

function HomeContent() {
  const {colorMode} = useColorMode();
  const lightLogoUrl = useBaseUrl('img/husky_logo_nobg.png');
  const darkLogoUrl = useBaseUrl('img/husky_logo_dark.png');
  const logoUrl = colorMode === 'dark' ? darkLogoUrl : lightLogoUrl;
  const [activeStep, setActiveStep] = useState(0);
  return (
    <>
      {/* ─── Hero ──────────────────────────────────────────────────────── */}
      <header className={styles.hero}>
        <div className={styles.heroBg} aria-hidden="true" />
        <div className={styles.heroInner}>
          <img src={logoUrl} alt="" className={styles.heroLogo} aria-hidden="true" />
          <p className={styles.eyebrow}>The scheduler that doesn't become an ops project</p>
          <h1 className={styles.heroTitle}>
            Workflows belong in your codebase<br />
            <span className={styles.heroAccent}>Version them. Review them. Ship them together.</span>
          </h1>
          <p className={styles.heroSub}>
            Husky is a project-scoped, Git-versioned workflow daemon. Define jobs in
            husky.yaml next to the app they automate, then review, diff, branch, and
            ship workflow changes with the rest of your code, without managing a
            separate platform.
          </p>
          <div className={styles.heroCtas}>
            <Link className={`button button--primary button--lg ${styles.ctaPrimary}`} to="/docs">
              Start the quickstart
            </Link>
            <Link className="button button--secondary button--lg" to="/docs/configuration">
              See configuration
            </Link>
            <a
              className={`button button--outline button--lg ${styles.ctaGh}`}
              href="https://github.com/husky-scheduler/husky"
              target="_blank"
              rel="noopener noreferrer"
            >
              ★ GitHub
            </a>
          </div>
          <div className={styles.heroInstall}>
            <span className={styles.installPrompt}>$</span>
            <code className={styles.installCode}>
              curl -sSL https://raw.githubusercontent.com/husky-scheduler/husky/main/install.sh | sh
            </code>
          </div>
        </div>
      </header>

      <main>
        {/* ─── Pillars ──────────────────────────────────────────────────────────── */}
        <section className={styles.pillarsSection}>
          <div className="container">
            <div className={styles.pillarsGrid}>
              {pillars.map((p) => (
                <div key={p.title} className={styles.pillarCard}>
                  <h3 className={styles.pillarTitle}>{p.title}</h3>
                  <p className={styles.pillarDesc}>{p.description}</p>
                </div>
              ))}
            </div>
          </div>
        </section>

        {/* ─── Features ─────────────────────────────────────────────────────────── */}
        <section className={styles.section}>
          <div className="container">
            <p className={styles.sectionLabel}>CAPABILITIES</p>
            <h2 className={styles.sectionTitle}>Everything you need to automate reliably</h2>
            <p className={styles.sectionSub}>
              From a simple cron replacement to multi-step pipelines with retries, SLA checks, and alerting.
            </p>
            <div className={styles.featureGrid}>
              {features.map((f) => (
                <Link key={f.title} to={f.link} className={styles.featureCard}>
                  <h3 className={styles.featureTitle}>{f.title}</h3>
                  <p className={styles.featureDesc}>{f.description}</p>
                  <span className={styles.learnMore}>Learn more →</span>
                </Link>
              ))}
            </div>
          </div>
        </section>

        {/* ─── Quickstart ───────────────────────────────────────────────────────── */}
        <section className={styles.sectionDark}>
          <div className="container">
            <p className={styles.sectionLabel}>QUICKSTART</p>
            <h2 className={styles.sectionTitle}>Up and running in 3 steps</h2>
            <p className={styles.sectionSub}>
              Everything installs with a single command and runs locally with no cloud account required.
            </p>
            <div className={styles.stepsLayout}>
              <div className={styles.stepsTabs}>
                {steps.map((s, i) => (
                  <button
                    key={s.num}
                    className={`${styles.stepTab} ${i === activeStep ? styles.stepTabActive : ''}`}
                    onClick={() => setActiveStep(i)}
                  >
                    <span className={styles.stepNum}>{s.num}</span>
                    <span className={styles.stepMeta}>
                      <strong>{s.title}</strong>
                      <small>{s.description}</small>
                    </span>
                  </button>
                ))}
              </div>
              <div className={styles.stepsCode}>
                <div className={styles.codeHeader}>
                  <span className={styles.codeLang}>{steps[activeStep].lang}</span>
                </div>
                <pre className={styles.codeBlock}>
                  <code>{steps[activeStep].code}</code>
                </pre>
              </div>
            </div>
          </div>
        </section>

        {/* ─── Doc Links ────────────────────────────────────────────────────────── */}
        <section className={styles.section}>
          <div className="container">
            <p className={styles.sectionLabel}>DOCS</p>
            <h2 className={styles.sectionTitle}>Jump to a guide</h2>
            <div className={styles.docGrid}>
              {docLinks.map((d) => (
                <Link key={d.label} to={d.to} className={styles.docCard}>
                  <strong className={styles.docCardTitle}>{d.label}</strong>
                  <span className={styles.docCardDesc}>{d.desc}</span>
                  <span className={styles.docCardArrow}>→</span>
                </Link>
              ))}
            </div>
          </div>
        </section>

        {/* ─── Community CTA ────────────────────────────────────────────────────── */}
        <section className={styles.communitySection}>
          <div className="container">
            <div className={styles.communityInner}>
              <h2 className={styles.communityTitle}>Built in the open, for everyone</h2>
              <p className={styles.communitySub}>
                Husky is MIT-licensed and developed entirely in public.
                Contributions, issues, and feature requests are welcome.
              </p>
              <div className={styles.communityActions}>
                <a
                  className="button button--primary button--lg"
                  href="https://github.com/husky-scheduler/husky"
                  target="_blank"
                  rel="noopener noreferrer"
                >
                  ★ Star on GitHub
                </a>
                <Link className={`button button--outline button--lg ${styles.ctaGh}`} to="/docs">
                  Read the docs
                </Link>
              </div>
            </div>
          </div>
        </section>
      </main>
    </>
  );
}
