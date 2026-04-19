# Swarmex Stateful

StatefulSet-like behavior for Docker Swarm — stable identity, ordered deploy, named volumes.

Part of [Swarmex](https://github.com/ccvass/swarmex) — enterprise-grade orchestration for Docker Swarm.

## What It Does

Brings Kubernetes-style StatefulSet semantics to Docker Swarm. Creates individually named service instances with stable identities (svc-0, svc-1, svc-2), each with its own persistent volume, deployed in order.

## Labels

```yaml
deploy:
  labels:
    swarmex.stateful.enabled: "true"              # Enable stateful behavior
    swarmex.stateful.replicas: "3"                # Number of instances
    swarmex.stateful.volume-template: "data-{i}"  # Volume name template ({i} = index)
    swarmex.stateful.ordered: "true"              # Deploy instances sequentially
```

## How It Works

1. Reads the stateful labels and determines the desired instance count.
2. Creates individual services named `<service>-0`, `<service>-1`, etc.
3. Creates a named volume per instance using the volume template.
4. When ordered is true, waits for each instance to be healthy before creating the next.
5. On scale-down, removes instances in reverse order (highest index first).

## Quick Start

```bash
docker service create \
  --name my-db \
  --label swarmex.stateful.enabled=true \
  --label swarmex.stateful.replicas=3 \
  --label swarmex.stateful.volume-template=db-data-{i} \
  --label swarmex.stateful.ordered=true \
  postgres:16
```

## Verified

3 instances (svc-0, svc-1, svc-2) created in order, each with its own dedicated volume.

## License

Apache-2.0
