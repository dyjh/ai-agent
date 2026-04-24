package ops

import "fmt"

// RollbackForTool builds a conservative rollback plan for an operations write tool.
func RollbackForTool(tool string, input map[string]any) RollbackPlan {
	switch tool {
	case "ops.local.service_restart", "ops.ssh.service_restart":
		service, _ := input["service"].(string)
		if service == "" {
			service, _ = input["service_name"].(string)
		}
		return RollbackPlan{
			CanRollback:          false,
			PreviousStateSummary: "previous service process state is not captured by this tool",
			RollbackSteps:        []string{fmt.Sprintf("inspect service %q status and logs", service), fmt.Sprintf("restart service %q again only after confirming it is safe", service)},
			RiskNote:             "service restart is a state-changing operation; previous in-memory process state cannot be restored automatically",
		}
	case "ops.docker.restart":
		container, _ := input["container"].(string)
		return RollbackPlan{
			CanRollback:          false,
			PreviousStateSummary: "container runtime state before restart is not snapshotted",
			RollbackSteps:        []string{fmt.Sprintf("inspect container %q status and recent logs", container), "restart the container again only if the new state is unhealthy"},
			RiskNote:             "docker restart cannot restore in-memory process state",
		}
	case "ops.docker.stop":
		container, _ := input["container"].(string)
		return RollbackPlan{
			CanRollback:          true,
			PreviousStateSummary: "container was expected to be running before stop",
			RollbackSteps:        []string{fmt.Sprintf("run ops.docker.start for container %q after approval", container)},
			RiskNote:             "starting the container may not restore in-memory sessions",
		}
	case "ops.docker.start":
		container, _ := input["container"].(string)
		return RollbackPlan{
			CanRollback:          true,
			PreviousStateSummary: "container was expected to be stopped before start",
			RollbackSteps:        []string{fmt.Sprintf("run ops.docker.stop for container %q after approval", container)},
			RiskNote:             "stopping after start is also a write operation and needs approval",
		}
	case "ops.k8s.apply":
		return RollbackPlan{
			CanRollback:          true,
			PreviousStateSummary: "previous live manifest is not captured by the tool unless the operator exports it first",
			RollbackSteps:        []string{"run kubectl rollout undo when supported by the resource", "or re-apply the previous manifest after approval"},
			BackupPaths:          nil,
			RiskNote:             "rollback depends on Kubernetes resource type and rollout history",
		}
	case "ops.k8s.delete":
		resource, _ := input["resource"].(string)
		name, _ := input["name"].(string)
		return RollbackPlan{
			CanRollback:          false,
			PreviousStateSummary: fmt.Sprintf("delete target %s/%s is not snapshotted by this tool", resource, name),
			RollbackSteps:        []string{"recreate the resource from a known-good manifest after approval"},
			RiskNote:             "delete is destructive; resources without backup manifests may not be recoverable",
		}
	case "ops.k8s.rollout_restart":
		return RollbackPlan{
			CanRollback:          true,
			PreviousStateSummary: "previous ReplicaSet may be available if rollout history exists",
			RollbackSteps:        []string{"run kubectl rollout undo for the workload after approval"},
			RiskNote:             "rollback depends on workload type and rollout history",
		}
	default:
		return RollbackPlan{
			CanRollback:          false,
			PreviousStateSummary: "previous state is not captured for this operation",
			RiskNote:             "no automatic rollback plan is available",
		}
	}
}
