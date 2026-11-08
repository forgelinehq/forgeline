// Forgeline web console: what Forgeline built, in words (Build › Built).
//
// No imports and no DOM, so it runs under Node in tests.

const TYPES = {
  aws_sqs_queue: 'SQS queue', aws_dynamodb_table: 'DynamoDB table', aws_s3_bucket: 'S3 bucket',
  aws_s3_bucket_public_access_block: 'Bucket public access block', aws_s3_bucket_ownership_controls: 'Bucket ownership',
  aws_s3_bucket_server_side_encryption_configuration: 'Bucket encryption', aws_s3_bucket_versioning: 'Bucket versioning',
  aws_cloudwatch_log_group: 'Log group', aws_lb: 'Load balancer', aws_lb_listener: 'Listener', aws_lb_target_group: 'Target group',
  aws_security_group: 'Security group', aws_vpc_security_group_ingress_rule: 'Inbound rule', aws_vpc_security_group_egress_rule: 'Outbound rule',
  aws_ecs_cluster: 'ECS cluster', aws_ecs_service: 'ECS service', aws_ecs_task_definition: 'Task definition',
  aws_iam_role: 'IAM role', aws_iam_role_policy: 'IAM role policy', aws_iam_role_policy_attachment: 'IAM policy attachment',
};

/** A resource type in words: aws_sqs_queue is "SQS queue". */
export function typeWords(type) {
  return TYPES[type] || String(type || '').replace(/^aws_/, '').replace(/_/g, ' ');
}

/** A blueprint's resources by block id, and the shared ones (not tied to one block) under ''. */
export function byBlock(bp) {
  const out = {};
  for (const r of bp.resources || []) (out[r.block || ''] ||= []).push(r);
  return out;
}

/** Where a set of resources stands after a check: missing (any is), present (all checked are), or unchecked. */
export function checkState(resources) {
  const checked = (resources || []).filter(r => r.present !== undefined && r.present !== null);
  if (!checked.length) return 'unchecked';
  return checked.some(r => !r.present) ? 'missing' : 'present';
}

/** The page's counts. */
export function totals(built) {
  const resources = (built.blueprints || []).reduce((n, b) => n + (b.resources || []).length, 0);
  const all = (built.blueprints || []).flatMap(b => b.resources || []);
  const missing = all.filter(r => r.present === false).length, checked = all.filter(r => r.present === true || r.present === false).length;
  return { blueprints: (built.blueprints || []).length, resources, stacks: (built.stacks || []).length, environments: (built.environments || []).length, missing, checked };
}

/** The best label for a resource: its name in AWS, else its id, else its address. */
export function resourceLabel(r) {
  return r.name || r.id || r.address;
}
