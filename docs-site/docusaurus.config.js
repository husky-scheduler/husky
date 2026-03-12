const {themes: prismThemes} = require('prism-react-renderer');

/** @type {import('@docusaurus/types').Config} */
const config = {
  title: 'Husky',
  tagline: 'Workflows should evolve with code and be managed like code.',
  url: 'https://husky-scheduler.github.io',
  baseUrl: '/',
  onBrokenLinks: 'throw',
  favicon: 'img/favicon.ico',
  organizationName: 'husky-scheduler',
  projectName: 'husky',
  trailingSlash: false,
  i18n: {
    defaultLocale: 'en',
    locales: ['en'],
  },
  markdown: {
    mermaid: true,
    hooks: {
      onBrokenMarkdownLinks: 'warn',
    },
  },
  themes: ['@docusaurus/theme-mermaid'],
  presets: [
    [
      'classic',
      {
        docs: {
          path: '../docs',
          routeBasePath: 'docs',
          sidebarPath: require.resolve('./sidebars.js'),
          editUrl: 'https://github.com/husky-scheduler/husky/tree/main/',
        },
        blog: false,
        theme: {
          customCss: require.resolve('./src/css/custom.css'),
        },
      },
    ],
  ],
  themeConfig: {
    image: 'img/husky_logo.png',
    metadata: [
      {name: 'description', content: 'Readable scheduling and workflow orchestration for local-first automation.'},
    ],
    navbar: {
      title: 'Husky',
      logo: {
        alt: 'Husky Logo',
        src: 'img/husky_logo_nobg.png',
        srcDark: 'img/husky_logo_dark.png',
      },
      hideOnScroll: false,
      items: [
        {to: '/docs', label: 'Docs', position: 'left'},
        {to: '/docs/getting-started/quickstart', label: 'Quickstart', position: 'left'},
        {to: '/docs/writing-workflows/yaml-reference', label: 'YAML Reference', position: 'left'},
        {to: '/docs/operations/overview', label: 'Operations', position: 'left'},
        {to: '/docs/operations/api', label: 'API', position: 'left'},
        {
          href: 'https://github.com/husky-scheduler/husky',
          position: 'right',
          className: 'header-github-link',
          'aria-label': 'GitHub repository',
        },
      ],
    },
    footer: {
      style: 'dark',
      logo: {
        alt: 'Husky',
        src: 'img/husky_logo_dark.png',
        width: 48,
        height: 48,
      },
      links: [
        {
          title: 'Learn',
          items: [
            {label: 'Overview', to: '/docs'},
            {label: 'Quickstart', to: '/docs/getting-started/quickstart'},
            {label: 'YAML reference', to: '/docs/writing-workflows/yaml-reference'},
            {label: 'Core concepts', to: '/docs/overview/core-concepts'},
          ],
        },
        {
          title: 'Operate',
          items: [
            {label: 'Operations', to: '/docs/operations/overview'},
            {label: 'Dashboard', to: '/docs/operations/dashboard'},
            {label: 'API', to: '/docs/operations/api'},
            {label: 'Crash recovery', to: '/docs/operations/crash-recovery'},
          ],
        },
        {
          title: 'Security & Testing',
          items: [
            {label: 'Security', to: '/docs/operations/security'},
            {label: 'Testing', to: '/docs/operations/testing'},
            {label: 'Tasks', to: '/docs/task'},
          ],
        },
        {
          title: 'Project',
          items: [
            {label: 'GitHub', href: 'https://github.com/husky-scheduler/husky'},
            {label: 'MIT License', href: 'https://github.com/husky-scheduler/husky/blob/main/LICENSE'},
          ],
        },
      ],
      copyright: `Copyright © ${new Date().getFullYear()} Husky contributors. Built with Docusaurus.`,
    },
    colorMode: {
      defaultMode: 'dark',
      respectPrefersColorScheme: true,
    },
    announcementBar: {
      id: 'husky_alpha',
      content: '🐺 Husky is under active development. Docs reflect current behavior.',
      backgroundColor: '#1e293b',
      textColor: '#94a3b8',
      isCloseable: true,
    },
    docs: {
      sidebar: {
        hideable: true,
        autoCollapseCategories: true,
      },
    },
    prism: {
      theme: prismThemes.github,
      darkTheme: prismThemes.dracula,
      additionalLanguages: ['yaml', 'bash', 'json'],
    },
    tableOfContents: {
      minHeadingLevel: 2,
      maxHeadingLevel: 4,
    },
  },
};

module.exports = config;
