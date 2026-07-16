import type {SidebarsConfig} from '@docusaurus/plugin-content-docs';

const sidebars: SidebarsConfig = {
  tutorialSidebar: [
    {
      type: 'category',
      label: 'Tutorials & Getting Started',
      items: [
        'tutorials/introduction',
        'tutorials/installation',
        'tutorials/quick-start',
        'tutorials/proxy-modes',
      ],
    },
    {
      type: 'category',
      label: 'How-to Guides',
      items: [
        'how-to/configuring-backends',
        'how-to/running-proxy',
        'how-to/docker',
        'how-to/cache-population',
        'how-to/authentication',
        'how-to/signing-keys',
        'how-to/github-action',
        'how-to/ci-integration',
        'how-to/troubleshooting',
      ],
    },
    {
      type: 'category',
      label: 'Reference',
      items: [
        {
          type: 'category',
          label: 'CLI Command Reference',
          link: {
            type: 'generated-index',
            description: 'Auto-generated reference documentation for Aeroflare CLI commands and flags.',
          },
          items: [
            'reference/cli/aeroflare',
            'reference/cli/aeroflare_init',
            'reference/cli/aeroflare_configure',
            'reference/cli/aeroflare_settings',
            'reference/cli/aeroflare_proxy',
            'reference/cli/aeroflare_run',
            'reference/cli/aeroflare_push',
            'reference/cli/aeroflare_prepare',
            'reference/cli/aeroflare_push-blob',
            'reference/cli/aeroflare_pull-blob',
            'reference/cli/aeroflare_auth',
            'reference/cli/aeroflare_auth_login',
            'reference/cli/aeroflare_auth_status',
            'reference/cli/aeroflare_auth_get',
            'reference/cli/aeroflare_auth_set',
            'reference/cli/aeroflare_auth_remove',
            'reference/cli/aeroflare_auth_import',
            'reference/cli/aeroflare_version',
          ],
        },
        'reference/configuration',
        'reference/ci-configuration',
        'reference/repository-layout',
        'reference/go-api',
      ],
    },
    {
      type: 'category',
      label: 'Concepts & Architecture',
      items: [
        'explanation/architecture',
        'explanation/oci-integration',
        'explanation/aeroflare-ci',
        'explanation/incremental-caching',
        'internals/architecture',
        'internals/subsystems',
        'internals/proxy-implementations',
        'internals/tasks-ui',
        {
          type: 'category',
          label: 'CLI Implementation Details',
          items: [
            'cli/core',
            'cli/auth',
            'cli/cache',
            'cli/maintenance',
          ],
        },
      ],
    },
    {
      type: 'category',
      label: 'Contributing',
      items: [
        'contributing/development',
      ],
    },
  ],
};

export default sidebars;
