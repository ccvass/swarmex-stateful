package stateful

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"
)

const (
	labelEnabled  = "swarmex.stateful.enabled"
	labelReplicas = "swarmex.stateful.replicas"
	labelVolTpl   = "swarmex.stateful.volume-template"
	labelOrdered  = "swarmex.stateful.ordered"
	labelManagedBy = "swarmex.stateful.managed-by" // marks child services
)

type Controller struct {
	client *client.Client
	logger *slog.Logger
}

func New(cli *client.Client, logger *slog.Logger) *Controller {
	return &Controller{client: cli, logger: logger}
}

func (c *Controller) HandleEvent(ctx context.Context, event events.Message) {
	if event.Type != events.ServiceEventType || event.Action != "create" {
		return
	}
	c.reconcile(ctx, event.Actor.ID)
}

func (c *Controller) reconcile(ctx context.Context, serviceID string) {
	svc, _, err := c.client.ServiceInspectWithRaw(ctx, serviceID, types.ServiceInspectOptions{})
	if err != nil {
		return
	}
	if svc.Spec.Labels[labelEnabled] != "true" || svc.Spec.Labels[labelManagedBy] != "" {
		return
	}

	replicas, _ := strconv.Atoi(svc.Spec.Labels[labelReplicas])
	if replicas < 1 {
		replicas = 1
	}
	volTpl := svc.Spec.Labels[labelVolTpl]
	ordered := svc.Spec.Labels[labelOrdered] == "true"

	c.logger.Info("creating stateful set", "service", svc.Spec.Name, "replicas", replicas, "ordered", ordered)

	// Scale template to 0
	zero := uint64(0)
	svc.Spec.Mode = swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: &zero}}
	c.client.ServiceUpdate(ctx, serviceID, svc.Version, svc.Spec, types.ServiceUpdateOptions{})

	// Save original mounts before loop (shallow copy shares pointers)
	origMounts := make([]mount.Mount, len(svc.Spec.TaskTemplate.ContainerSpec.Mounts))
	copy(origMounts, svc.Spec.TaskTemplate.ContainerSpec.Mounts)
	origLabels := make(map[string]string)
	for k, v := range svc.Spec.Labels {
		origLabels[k] = v
	}

	for i := 0; i < replicas; i++ {
		spec := svc.Spec
		spec.Name = fmt.Sprintf("%s-%d", svc.Spec.Name, i)
		one := uint64(1)
		spec.Mode = swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: &one}}

		// Fresh label copy per iteration
		labels := make(map[string]string)
		for k, v := range origLabels {
			labels[k] = v
		}
		labels[labelManagedBy] = svc.Spec.Name
		delete(labels, labelEnabled)
		spec.Labels = labels

		// Fresh mount copy per iteration
		mounts := make([]mount.Mount, len(origMounts))
		copy(mounts, origMounts)
		if volTpl != "" {
			volName := strings.ReplaceAll(volTpl, "{index}", strconv.Itoa(i))
			mounts = append(mounts, mount.Mount{Type: mount.TypeVolume, Source: volName, Target: "/data"})
		}
		spec.TaskTemplate.ContainerSpec.Mounts = mounts

		_, err := c.client.ServiceCreate(ctx, spec, types.ServiceCreateOptions{})
		if err != nil {
			c.logger.Error("stateful instance create failed", "name", spec.Name, "error", err)
			return
		}
		c.logger.Info("stateful instance created", "name", spec.Name, "index", i)

		if ordered && i < replicas-1 {
			c.waitHealthy(ctx, spec.Name, 60*time.Second)
		}
	}
}

func (c *Controller) waitHealthy(ctx context.Context, serviceName string, timeout time.Duration) {
	deadline := time.After(timeout)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			services, err := c.client.ServiceList(ctx, types.ServiceListOptions{
				Filters: filters.NewArgs(filters.Arg("name", serviceName)),
			})
			if err != nil || len(services) == 0 {
				continue
			}
			tasks, _ := c.client.TaskList(ctx, types.TaskListOptions{
				Filters: filters.NewArgs(filters.Arg("service", services[0].ID), filters.Arg("desired-state", "running")),
			})
			for _, t := range tasks {
				if t.Status.State == swarm.TaskStateRunning {
					return
				}
			}
		case <-deadline:
			c.logger.Warn("timeout waiting for healthy", "service", serviceName)
			return
		case <-ctx.Done():
			return
		}
	}
}
