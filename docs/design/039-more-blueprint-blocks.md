# 039: More blueprint blocks: databases, Redis, secrets, service calls, scaling and paths

Status: **Design (2026-11-08), being built.** Extends note 035.

## Question

A maintainer asked the canvas assistant for "a React frontend and a PHP backend with data in RDS MySQL, a queue worker
that scales, secrets and Redis". It could not: blueprints had no database, no Redis, no secret, no way for one service to
call another, no autoscaling, and a front door could reach only one service. What blocks and links close that gap,
without Forgeline ever seeing a password?

## Decisions

| Need | Block or link | What is built (OpenTofu, AWS provider) | Safe by default |
| --- | --- | --- | --- |
| A relational database | **database** block: `engine` mysql or postgres, `size` (small, medium, large: db.t4g.micro/small/medium), `storage_gb` 20 to 1000, `multi_az`, `backup_days` 1 to 35 | `aws_db_instance` in a subnet group of the default VPC's subnets, with `manage_master_user_password`: **AWS creates and keeps the password in Secrets Manager**; Forgeline never sees it | Encrypted, not public, deletion protection, `prevent_destroy`, a final snapshot, backups 7 days by default |
| Redis | **cache** block: `size` (small, medium: cache.t4g.micro/small), `nodes` 1 to 3 | `aws_elasticache_replication_group` (Redis OSS) in a subnet group, encryption at rest and in transit | Not public; only linked services may connect |
| A secret the app needs | **secret** block: a name and a description | `aws_secretsmanager_secret` **with no value**: a person sets the value in AWS (the page says where); Forgeline never reads or stores it | `prevent_destroy`; only linked services may read it |
| A service uses a database | link service → database (`access` readwrite) | The database's security group accepts the service on its port; the container gets `DB_HOST`, `DB_PORT`, `DB_NAME`, and `DB_USERNAME` and `DB_PASSWORD` **injected by ECS from the AWS-managed secret** at start; the task's execution role may read only that secret | |
| A service uses Redis | link service → cache | Port 6379 opened to the service only; `REDIS_HOST`, `REDIS_PORT` | TLS on |
| A service reads a secret | link service → secret | ECS injects it as an environment variable named after the block (`SECRET_<ID>`), from Secrets Manager at start; only that secret's ARN is readable | The value never passes through Forgeline |
| A service calls another | link service → service (`calls`) | A private DNS namespace (Cloud Map) for the blueprint; the called service registers there; its security group accepts the caller on its port; the caller gets `<ID>_URL=http://<id>.<namespace>:<port>` | The called service stays private |
| Scaling | service fields `min`, `max` (1 to 20), `cpu_target` (percent) | `aws_appautoscaling_target` and a target-tracking policy on average CPU; `count` is the starting number | Only when `max` is above `min` |
| One door, several services | link front door → service with a `path` (for example `/api/*`) | A listener rule per path to that service's target group; the link without a path is the default | At most 10 paths; exactly one default |
| Using what exists | `existing` also takes `database`, `cache` and `secret` | Read at plan time by name, like tables and buckets | Never changed |

Checks in plain words gain: what holds data and is kept (databases with deletion protection and backups), that passwords
live in Secrets Manager and Forgeline never sees them, which secrets have no value yet, which services can call which,
and how far each service scales.

The canvas assistant's instructions list the new blocks and links, so the conversation above can be drawn.

## Not yet

Aurora, read replicas, DynamoDB streams, SNS, CloudFront, custom domains and certificates per path, autoscaling on
anything but CPU, and setting a secret's value from Forgeline (deliberately: it would make Forgeline a holder of secrets).
