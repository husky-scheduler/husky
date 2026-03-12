/** @type {import('@docusaurus/plugin-content-docs').SidebarsConfig} */
const sidebars = {
  docsSidebar: [
    'index',
    {
      type: 'category',
      label: 'Overview',
      items: [
        'overview/introduction',
        'overview/architecture',
        'overview/core-concepts',
      ],
    },
    {
      type: 'category',
      label: 'Getting started',
      items: [
        'getting-started/installation',
        'getting-started/quickstart',
        'getting-started/cli',
      ],
    },
    {
      type: 'category',
      label: 'Writing workflows',
      items: [
        'writing-workflows/overview',
        'writing-workflows/yaml-reference',
        'writing-workflows/scheduling',
        'writing-workflows/dependencies',
        'writing-workflows/retries-and-concurrency',
        'writing-workflows/output-passing',
        'writing-workflows/healthchecks-and-slas',
        'writing-workflows/notifications',
        'writing-workflows/tags-audit-and-timezones',
      ],
    },
    {
      type: 'category',
      label: 'Operations',
      items: [
        'operations/overview',
        'operations/dashboard',
        'operations/api',
        'operations/crash-recovery',
        'operations/security',
        'operations/testing',
        'operations/packaging',
      ],
    },
    {
      type: 'category',
      label: 'Project',
      items: [
        'task',
      ],
    },
  ],
};

module.exports = sidebars;
