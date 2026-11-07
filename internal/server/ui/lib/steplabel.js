// Forgeline web console: what an assistant lookup says in plain words (design note 029).
//
// No imports and no DOM, so it runs under Node in tests. The technical name and arguments stay
// available behind the step for anyone who wants them; this is only the sentence people read.

const VERBS = {
  list_runs: ['Looked at recent runs', 'Looking at recent runs'],
  get_run: ['Read a run', 'Reading a run'],
  get_step_log: ['Read a step’s log', 'Reading a step’s log'],
  run_failure: ['Worked out why a run failed', 'Working out why a run failed'],
  list_pipelines: ['Listed your pipelines', 'Listing your pipelines'],
  get_pipeline: ['Read a pipeline', 'Reading a pipeline'],
  aws_accounts: ['Checked which AWS accounts are connected', 'Checking which AWS accounts are connected'],
  aws_resources: ['Listed AWS resources', 'Listing AWS resources'],
  aws_summary: ['Read the AWS summary', 'Reading the AWS summary'],
  call_aws: ['Asked AWS a question', 'Asking AWS a question'],
  aws_script: ['Read from AWS', 'Reading from AWS'],
  propose_aws_change: ['Drafted a change for approval', 'Drafting a change for approval'],
  propose_cloud_change: ['Drafted a stack or instance change for approval', 'Drafting a stack or instance change for approval'],
  k8s_clusters: ['Checked the Kubernetes clusters', 'Checking the Kubernetes clusters'],
  k8s_resources: ['Looked inside a Kubernetes cluster', 'Looking inside a Kubernetes cluster'],
  gcp_resources: ['Listed Google Cloud resources', 'Listing Google Cloud resources'],
  azure_resources: ['Listed Azure resources', 'Listing Azure resources'],
  morning_check: ['Did the morning check', 'Doing the morning check'],
  github_repos: ['Listed your GitHub repositories', 'Listing your GitHub repositories'],
  github_files: ['Read files from GitHub', 'Reading files from GitHub'],
  guard_findings: ['Read the security findings', 'Reading the security findings'],
  environment_templates: ['Looked at environment templates', 'Looking at environment templates'],
  request_environment: ['Requested an environment', 'Requesting an environment'],
  list_agents: ['Listed your agents', 'Listing your agents'],
  ask_agent: ['Asked an agent', 'Asking an agent'],
  remember: ['Saved a note to memory', 'Saving a note to memory'],
  prepare_action: ['Prepared something for you to confirm', 'Preparing something for you to confirm'],
  search_audit: ['Searched the audit log', 'Searching the audit log'],
  aws_search_documentation: ['Searched the AWS documentation', 'Searching the AWS documentation'],
  aws_read_documentation: ['Read AWS documentation', 'Reading AWS documentation'],
  aws_retrieve_skill: ['Loaded an AWS procedure', 'Loading an AWS procedure'],
  aws_list_regions: ['Listed AWS regions', 'Listing AWS regions'],
  aws_get_regional_availability: ['Checked where an AWS service is available', 'Checking where an AWS service is available'],
};

// Argument keys worth showing, in the order they read best.
const DETAIL_KEYS = ['account', 'service', 'cluster', 'kind', 'pipeline', 'repo', 'path', 'severity', 'agent', 'template', 'title', 'search_phrase'];

function parseArgs(args) {
  if (!args) return null;
  try { const v = JSON.parse(args); return v && typeof v === 'object' && !Array.isArray(v) ? v : null; } catch { return null; }
}

/**
 * The sentence for one step. status is running, done or failed.
 * Returns { text, detail } where detail is a short "primary · sqs" style note, or ''.
 */
export function stepLabel(name, args, status) {
  const v = VERBS[name];
  const running = status === 'running';
  let text = v ? v[running ? 1 : 0] : (running ? 'Using ' : 'Used ') + String(name || 'a tool').replace(/_/g, ' ');
  if (status === 'failed') text = 'Could not finish: ' + text.charAt(0).toLowerCase() + text.slice(1);
  const a = parseArgs(args);
  const parts = [];
  if (a) {
    for (const k of DETAIL_KEYS) {
      const val = a[k];
      if (typeof val === 'string' && val.trim()) parts.push(val.trim().slice(0, 40));
      if (parts.length === 3) break;
    }
  }
  return { text, detail: parts.join(' · ') };
}

/** The line above a finished set of steps: "Looked at 3 things" or "Looked at 1 thing (1 could not finish)". */
export function stepsSummary(steps) {
  const n = steps.length;
  const failed = steps.filter(s => s.status === 'failed').length;
  const running = steps.some(s => s.status === 'running');
  const head = running ? 'Working' : 'Looked at ' + n + ' thing' + (n === 1 ? '' : 's');
  return failed ? head + ' (' + failed + ' could not finish)' : head;
}
