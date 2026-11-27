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
  blueprints_built: ['Read what the blueprints built', 'Reading what the blueprints built'],
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
  inspector_findings: ['Read the Amazon Inspector findings', 'Reading the Amazon Inspector findings'],
  run_runbook: ['Ran a runbook', 'Running a runbook'],
  ask_user: ['Asked you a question', 'Asking you a question'],
  run_shell: ['Ran a command in its sandbox', 'Running a command in its sandbox'],
  offer_file: ['Offered a file for download', 'Offering a file for download'],
  org_memory: ['Read the organisation’s notes', 'Reading the organisation’s notes'],
  update_plan: ['Updated its plan', 'Updating its plan'],
  environment_templates: ['Looked at environment templates', 'Looking at environment templates'],
  request_environment: ['Requested an environment', 'Requesting an environment'],
  list_agents: ['Listed your agents', 'Listing your agents'],
  search_docs: ['Read the documentation', 'Reading the documentation'],
  list_teams: ['Listed your teams', 'Listing your teams'],
  get_team: ['Read a team and its runs', 'Reading a team'],
  list_workflows: ['Listed your workflows', 'Listing your workflows'],
  get_workflow: ['Read a workflow', 'Reading a workflow'],
  list_missions: ['Listed the missions', 'Listing the missions'],
  get_mission: ['Read a mission', 'Reading a mission'],
  list_models: ['Listed the models', 'Listing the models'],
  ask_agent: ['Asked an agent', 'Asking an agent'],
  remember: ['Saved a note to memory', 'Saving a note to memory'],
  memory: ['Worked with its memory files', 'Working with its memory files'],
  forgeline_api: ['Changed something in Forgeline', 'Working in Forgeline'],
  doctor: ['Checked what is held back in its setup', 'Checking what is held back in its setup'],
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
  // The API tool both reads and changes. A read says so. Arguments too long to parse carry a body, so they are a change.
  const a = parseArgs(args);
  const reads = name === 'forgeline_api' && !!a && String(a.method || '').trim().toUpperCase() === 'GET';
  const v = reads ? ['Read from Forgeline', 'Reading from Forgeline'] : VERBS[name];
  const running = status === 'running';
  let text = v ? v[running ? 1 : 0] : (running ? 'Using ' : 'Used ') + String(name || 'a tool').replace(/_/g, ' ');
  if (status === 'failed') text = 'Could not finish: ' + text.charAt(0).toLowerCase() + text.slice(1);
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

const FILE_TOOLS = ['Write', 'Edit', 'MultiEdit', 'NotebookEdit'];

/**
 * What a permission card says. It depends on what is being asked: a command for the agent's shell, a file change, a web
 * request, an AWS change, or secrets for a command in the sandbox. It is only about AWS when it is an AWS change.
 */
export function permissionWording(x) {
  const t = x.tool || '';
  if (t === 'run_shell') return { title: 'Give secrets to this command?', lead: 'The assistant wants to run this in its sandbox, with secrets from your vault. Nothing has run yet.' };
  if (t === 'Bash') return { title: 'Allow this command?', lead: 'The agent wants to run this command. Nothing has run yet.' };
  if (FILE_TOOLS.includes(t)) return { title: 'Allow this file change?', lead: 'The agent wants to change a file in its workspace. Nothing has changed yet.' };
  if (t === 'forgeline_api') return { title: 'Allow this change in Forgeline?', lead: 'The assistant wants to change this in Forgeline, as you. Nothing has changed yet.' };
  if (t === 'WebFetch' || t === 'WebSearch') return { title: 'Allow this web request?', lead: 'The agent wants to reach the web. Nothing has been sent yet.' };
  if (t === 'call_aws' || x.account) return { title: 'Allow this change?', lead: 'The assistant wants to run this on ' + (x.account === 'primary' ? 'the primary AWS connection' : 'AWS account ' + x.account) + ', as you. Nothing has changed yet.' };
  return { title: 'Allow this?', lead: 'The agent wants to use its ' + (t || 'tool') + '. Nothing has run yet.' };
}
