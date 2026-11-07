// Forgeline web console: agent tools in plain words, grouped by what they touch (design note 029).
//
// No imports and no DOM, so it runs under Node in tests. The technical name is still shown beside
// each title; this only decides the order, the group and the words people read first.

export const TOOL_GROUPS = [
  { id: 'delivery', title: 'Pipelines and runs', note: 'Read pipelines, runs and logs' },
  { id: 'aws', title: 'AWS', note: 'Read your AWS accounts; propose changes for approval' },
  { id: 'kubernetes', title: 'Kubernetes', note: 'Read registered clusters' },
  { id: 'cloud', title: 'Google Cloud and Azure', note: 'Read the connected projects and subscriptions' },
  { id: 'github', title: 'GitHub', note: 'Read the repositories an administrator allowed' },
  { id: 'security', title: 'Security and health', note: 'Guard findings and the morning check' },
  { id: 'work', title: 'Environments, agents and memory', note: 'Ask for environments, hand work to agents, remember things' },
  { id: 'docs', title: 'AWS documentation', note: 'Search and read AWS’s public documentation' },
  { id: 'other', title: 'Other', note: '' },
];

const TOOLS = {
  list_runs: ['delivery', 'See recent runs'],
  get_run: ['delivery', 'Read one run'],
  get_step_log: ['delivery', 'Read a step’s log'],
  run_failure: ['delivery', 'Explain why a run failed'],
  list_pipelines: ['delivery', 'See saved pipelines'],
  get_pipeline: ['delivery', 'Read a pipeline file'],
  prepare_action: ['delivery', 'Prepare a pipeline or a run for you to confirm'],
  aws_accounts: ['aws', 'See connected AWS accounts'],
  aws_resources: ['aws', 'List AWS resources by service'],
  aws_summary: ['aws', 'Read the AWS summary'],
  call_aws: ['aws', 'Ask AWS one read-only question'],
  aws_script: ['aws', 'Run read-only AWS API calls'],
  propose_aws_change: ['aws', 'Draft an AWS change for approval'],
  propose_cloud_change: ['aws', 'Draft a stack or instance change for approval'],
  k8s_clusters: ['kubernetes', 'See registered clusters'],
  k8s_resources: ['kubernetes', 'Look inside a cluster'],
  gcp_resources: ['cloud', 'List Google Cloud resources'],
  azure_resources: ['cloud', 'List Azure resources'],
  github_repos: ['github', 'See allowed repositories'],
  github_files: ['github', 'Read files in a repository'],
  guard_findings: ['security', 'Read security findings'],
  morning_check: ['security', 'Do the morning check'],
  search_audit: ['security', 'Search the audit log'],
  environment_templates: ['work', 'See environment templates'],
  request_environment: ['work', 'Ask for an environment'],
  list_agents: ['work', 'See other agents'],
  ask_agent: ['work', 'Hand a task to another agent'],
  remember: ['work', 'Remember a note'],
  aws_search_documentation: ['docs', 'Search AWS documentation'],
  aws_read_documentation: ['docs', 'Read an AWS documentation page'],
  aws_retrieve_skill: ['docs', 'Load an AWS procedure'],
  aws_list_regions: ['docs', 'List AWS regions'],
  aws_get_regional_availability: ['docs', 'Check where an AWS service is available'],
};

// Tools that ask for something a person must approve.
const ASKS = new Set(['propose_aws_change', 'propose_cloud_change', 'request_environment']);

/** A tool's plain title and group. Unknown tools keep their name and go to Other. */
export function toolInfo(name) {
  const t = TOOLS[name];
  return { group: t ? t[0] : 'other', title: t ? t[1] : String(name).replace(/_/g, ' '), asks: ASKS.has(name) };
}

/**
 * Tools (as GET /v1/ai/tools returns them: { name, help, min, needs }) sorted into the groups above,
 * keeping the server's order inside a group and leaving out empty groups.
 */
export function groupTools(tools) {
  const by = new Map(TOOL_GROUPS.map(g => [g.id, []]));
  for (const t of tools || []) by.get(toolInfo(t.name).group).push({ ...t, ...toolInfo(t.name) });
  return TOOL_GROUPS.map(g => ({ ...g, tools: by.get(g.id) })).filter(g => g.tools.length);
}

/**
 * A tool's description without the argument schema it starts with (the model needs that, people do
 * not). Mirrors toolHelp on the server: drop everything up to the first "} ", then cut at 140.
 */
export function plainHelp(help) {
  let t = String(help || '');
  if (t.startsWith('{')) { const i = t.indexOf('} '); if (i > 0) t = t.slice(i + 2); }
  t = t.trim();
  return t.length > 140 ? t.slice(0, 139) + '…' : t;
}

/** What a tier lets an agent do, in words. */
export const TIERS = [
  { id: 'T0', title: 'Advises', text: 'Reads and explains. It never asks for a change.' },
  { id: 'T1', title: 'Proposes', text: 'Can also draft cloud changes and ask for environments. A person approves every one.' },
];

export function tierTitle(t) { return (TIERS.find(x => x.id === t) || TIERS[0]).title; }
