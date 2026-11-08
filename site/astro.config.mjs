// The Forgeline documentation site. Pages live in ../docs; `npm run build` copies them in (scripts/sync.mjs) and
// builds a static site for GitHub Pages under /forgeline.
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';

export default defineConfig({
  site: 'https://forgelinehq.github.io',
  base: '/forgeline',
  trailingSlash: 'always',
  integrations: [
    starlight({
      title: 'Forgeline',
      description: 'Delivery where AI agents run safely: pipelines, cloud changes and an AI team, self-hosted, with a person approving every change.',
      logo: { src: './src/assets/logo.svg', alt: 'Forgeline' },
      favicon: '/favicon.svg',
      social: [{ icon: 'github', label: 'GitHub', href: 'https://github.com/forgelinehq/forgeline' }],
      customCss: ['./src/styles/custom.css'],
      components: { PageTitle: './src/components/PageTitle.astro' },
      lastUpdated: false,
      sidebar: [
        { label: 'Start here', items: [{ label: 'Overview', link: '/' }, 'get-started', 'status'] },
        {
          label: 'Guides',
          items: [
            { label: 'All guides', slug: 'guides' }, 'guides/console-tour', 'guides/pipelines', 'guides/assistant-and-agents', 'guides/people-and-security',
            'guides/connect-github', 'guides/github-actions', 'guides/connect-aws', 'guides/connect-gcp', 'guides/connect-azure', 'guides/versioning',
          ],
        },
        { label: 'Reference', items: ['api', 'api/reference', { label: 'Screenshots', slug: 'console' }] },
        { label: 'Project', items: ['roadmap', 'architecture'] },
        { label: 'Design notes', collapsed: true, items: [{ autogenerate: { directory: 'design' } }] },
      ],
    }),
  ],
});
