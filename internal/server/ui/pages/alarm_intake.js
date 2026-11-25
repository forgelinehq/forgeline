// Settings › Connections › Alarm intake (design note 051): CloudWatch alarms reach workflows through an SQS queue that Forgeline
// polls. Nothing needs to reach the server from outside; the queue is the only door, and its policy lets only SNS in.
import { h, icon, mount, pageHeader, spinner, errorBox, notice, toast, field, relTime, copyButton } from '../lib/dom.js';
import { get, put, post } from '../lib/api.js';

const SETUP = (region) => `# 1. A queue, and a topic that alarms publish to (change the names if you like)
REGION=${region || 'us-east-1'}
QUEUE_URL=$(aws sqs create-queue --region $REGION --queue-name forgeline-alarms --query QueueUrl --output text)
QUEUE_ARN=$(aws sqs get-queue-attributes --region $REGION --queue-url $QUEUE_URL --attribute-names QueueArn --query Attributes.QueueArn --output text)
TOPIC_ARN=$(aws sns create-topic --region $REGION --name forgeline-alarms --query TopicArn --output text)

# 2. Only this topic may send to the queue
aws sqs set-queue-attributes --region $REGION --queue-url $QUEUE_URL --attributes "{\\"Policy\\":\\"{\\\\\\"Version\\\\\\":\\\\\\"2012-10-17\\\\\\",\\\\\\"Statement\\\\\\":[{\\\\\\"Effect\\\\\\":\\\\\\"Allow\\\\\\",\\\\\\"Principal\\\\\\":{\\\\\\"Service\\\\\\":\\\\\\"sns.amazonaws.com\\\\\\"},\\\\\\"Action\\\\\\":\\\\\\"sqs:SendMessage\\\\\\",\\\\\\"Resource\\\\\\":\\\\\\"$QUEUE_ARN\\\\\\",\\\\\\"Condition\\\\\\":{\\\\\\"ArnEquals\\\\\\":{\\\\\\"aws:SourceArn\\\\\\":\\\\\\"$TOPIC_ARN\\\\\\"}}}]}\\"}"

# 3. The topic delivers to the queue, and an alarm publishes to the topic
aws sns subscribe --region $REGION --topic-arn $TOPIC_ARN --protocol sqs --notification-endpoint $QUEUE_ARN
# in each alarm: --alarm-actions $TOPIC_ARN

echo $QUEUE_URL   # paste this above`;

export async function alarmIntakePage() {
  const el = h('div', {});
  async function load() {
    let v;
    try { v = await get('/v1/settings/alarm-intake'); } catch (e) { return mount(el, pageHeader('Alarm intake', ''), errorBox(e.message)); }
    const enabled = h('input', { type: 'checkbox', id: 'alarm-enabled', class: 'mt-1', checked: v.enabled });
    const queue = h('input', { id: 'alarm-queue', class: 'input font-mono', value: v.queue_url || '', placeholder: 'https://sqs.us-east-1.amazonaws.com/123456789012/forgeline-alarms', spellcheck: 'false' });
    const out = h('div', { id: 'alarm-result' });
    const save = h('button', { class: 'btn-primary', type: 'button', id: 'alarm-save', onclick: async () => {
      save.disabled = true;
      try { await put('/v1/settings/alarm-intake', { enabled: enabled.checked, queue_url: queue.value.trim() }); toast('Saved', 'ok'); load(); }
      catch (e) { mount(out, errorBox(e.message)); save.disabled = false; }
    } }, icon('check', 'size-4'), 'Save');
    const test = h('button', { class: 'btn-secondary', type: 'button', id: 'alarm-test', disabled: !v.queue_url, onclick: async () => {
      test.disabled = true; mount(out, spinner());
      try { const r = await post('/v1/settings/alarm-intake/test'); mount(out, notice('ok', 'The queue can be read (' + r.queue_arn + '). ' + r.waiting + ' message(s) waiting.')); }
      catch (e) { mount(out, errorBox(e.message)); }
      test.disabled = false;
    } }, icon('bolt', 'size-4'), 'Test the queue');
    const s = v.status || {};
    const script = SETUP(v.region);
    mount(el,
      pageHeader('Alarm intake', 'Start a workflow when a CloudWatch alarm goes off: alarm, SNS topic, SQS queue, and Forgeline reads the queue.'),
      h('div', { class: 'grid gap-4 lg:grid-cols-[minmax(0,1fr)_20rem]' },
        h('section', { class: 'card card-pad space-y-4' },
          !v.aws_connected ? notice('warn', 'AWS is not connected. The intake reads the queue with the AWS connection\'s credentials (Settings › Connections).') : null,
          h('label', { class: 'flex items-start gap-2.5 text-sm', for: 'alarm-enabled' }, enabled, h('span', {}, h('span', { class: 'block font-medium' }, 'Read the queue'),
            h('span', { class: 'muted block text-xs' }, 'Forgeline asks the queue for messages, so nothing has to reach this server from the internet.'))),
          field('Queue address', queue, 'The queue URL in the account of the AWS connection. Only alarms from that account are acted on.', 'alarm-queue'),
          h('div', { class: 'flex flex-wrap items-center gap-2' }, save, test), out,
          notice('info', 'A message that is an alarm going to ALARM starts every workflow whose trigger names it (Workflow settings › Trigger), at most six runs an hour for each workflow. The alarm\'s details reach the workflow as its trigger, which agents read as data and never as instructions.'),
          h('details', { class: 'text-sm' }, h('summary', { class: 'cursor-pointer font-medium' }, 'Set up the queue, the topic and the permission (AWS CLI)'),
            h('p', { class: 'muted my-2 text-xs' }, 'The queue\'s policy is the lock: it lets only this SNS topic send. Anyone who can send to the queue can start workflows, so do not loosen it.'),
            h('div', { class: 'relative' }, h('pre', { class: 'max-h-80 overflow-auto whitespace-pre-wrap break-all rounded-lg bg-slate-50 p-3 font-mono text-[11px] dark:bg-slate-900' }, script),
              h('div', { class: 'absolute right-2 top-2' }, copyButton(script, 'Copy the commands'))))),
        h('aside', { class: 'card card-pad space-y-2 text-sm', id: 'alarm-status' },
          h('h2', { class: 'text-base font-semibold' }, 'Status'),
          h('div', {}, s.polling ? h('span', { class: 'pill-ok' }, 'Reading the queue') : h('span', { class: 'pill-idle' }, 'Off')),
          h('p', { class: 'muted text-xs' }, s.last_poll && s.last_poll !== '0001-01-01T00:00:00Z' ? 'Last read ' + relTime(s.last_poll) : 'Not read yet'),
          s.last_error ? errorBox(s.last_error) : null,
          h('dl', { class: 'grid grid-cols-2 gap-1 text-xs' }, h('dt', { class: 'muted' }, 'Messages read'), h('dd', {}, String(s.received || 0)), h('dt', { class: 'muted' }, 'Runs started'), h('dd', {}, String(s.started || 0)), h('dt', { class: 'muted' }, 'Ignored'), h('dd', {}, String(s.ignored || 0))),
          v.updated_by ? h('p', { class: 'muted text-xs' }, 'Changed by ' + v.updated_by) : null)));
  }
  mount(el, spinner());
  await load();
  return el;
}
