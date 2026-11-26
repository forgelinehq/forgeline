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
            { label: 'All guides', slug: 'guides' },
            { label: 'Console', collapsed: true, items: ['guides/console-basics', 'guides/home-and-approvals'] },
            { label: 'Pipelines and delivery', collapsed: true, items: ['guides/pipelines', 'guides/pipeline-editor-and-runs', 'guides/github-actions', 'guides/versioning', 'guides/agent-steps'] },
            { label: 'The assistant', collapsed: true, items: ['guides/assistant', 'guides/assistant-permissions-and-autopilot', 'guides/assistant-sandbox', 'guides/assistant-agent-engines', 'guides/assistant-memory', 'guides/mcp-servers'] },
            { label: 'Agents and workflows', collapsed: true, items: ['guides/agents', 'guides/agent-builder', 'guides/agent-bundles', 'guides/publishing-agent-versions', 'guides/evals', 'guides/teams', 'guides/schedules', 'guides/workflows', 'guides/workflow-canvas', 'guides/missions', 'guides/ai-use-and-budgets', 'guides/models-and-providers'] },
            { label: 'Cloud and operations', collapsed: true, items: ['guides/cloud-canvas', 'guides/blueprints-and-environments', 'guides/infrastructure-map-and-services', 'guides/runbooks', 'guides/alarm-intake', 'guides/traces-and-opentelemetry'] },
            { label: 'Security and access', collapsed: true, items: ['guides/people-and-roles', 'guides/single-sign-on', 'guides/approval-rules', 'guides/policy-packs', 'guides/audit-log', 'guides/secrets-and-memory', 'guides/connections-and-ai-access'] },
            { label: 'Connect your accounts', collapsed: true, items: ['guides/connect-github', 'guides/connect-aws', 'guides/connect-gcp', 'guides/connect-azure'] },
          ],
        },
        { label: 'Reference', items: ['api', 'api/reference'] },
        { label: 'Project', items: ['roadmap', 'architecture'] },
        { label: 'Design notes', collapsed: true, items: [{ autogenerate: { directory: 'design' } }] },
      ],
    }),
  ],
});
