package v2beta1

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	emperror "emperror.dev/errors"
	appsv2beta1 "github.com/emqx/emqx-operator/apis/apps/v2beta1"
	innerReq "github.com/emqx/emqx-operator/internal/requester"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const clusterRescueMinPodAge = 120 * time.Second

type clusterRescue struct {
	*EMQXReconciler
}

func (c *clusterRescue) reconcile(ctx context.Context, logger logr.Logger, instance *appsv2beta1.EMQX, _ innerReq.RequesterInterface) subResult {
	if instance.Status.IsConditionTrue(appsv2beta1.Available) {
		return subResult{}
	}

	pods := &corev1.PodList{}
	if err := c.Client.List(ctx, pods,
		client.InNamespace(instance.Namespace),
		client.MatchingLabels(appsv2beta1.DefaultLabels(instance)),
	); err != nil {
		return subResult{err: emperror.Wrap(err, "failed to list pods for cluster rescue")}
	}

	runningPods := filterRunningPods(pods.Items)
	if len(runningPods) == 0 {
		return subResult{}
	}

	now := time.Now()
	for _, pod := range runningPods {
		if ok, reason := podEligibleForRescue(pod, now); !ok {
			logger.V(1).Info("cluster rescue skipped - pod not ready", "pod", pod.Name, "reason", reason)
			return subResult{}
		}
	}

	for _, pod := range runningPods {
		hasPending, err := c.podHasPendingTables(ctx, logger, pod)
		if err != nil {
			return subResult{err: emperror.Wrapf(err, "failed to check pending tables in pod %s", pod.Name)}
		}
		if !hasPending {
			logger.V(1).Info("cluster rescue aborted - pod without pending tables", "pod", pod.Name)
			return subResult{}
		}
	}

	target := selectRescueTarget(runningPods)
	stdout, stderr, err := c.execEmqxCommandWithResult(ctx, logger, target, []string{"emqx_cluster_rescue", "force-load"})
	if err != nil {
		return subResult{err: emperror.Wrapf(err, "failed to force-load pending tables in pod %s", target.Name)}
	}

	logger.Info("executed emqx_cluster_rescue force-load", "pod", target.Name, "stdout", strings.TrimSpace(stdout), "stderr", strings.TrimSpace(stderr))
	c.EventRecorder.Eventf(instance, corev1.EventTypeNormal, "ClusterRescueForceLoad", "Issued emqx_cluster_rescue force-load on pod %s", target.Name)
	return subResult{}
}

func (c *clusterRescue) podHasPendingTables(ctx context.Context, logger logr.Logger, pod *corev1.Pod) (bool, error) {
	stdout, _, err := c.execEmqxCommandWithResult(ctx, logger, pod, []string{"emqx_cluster_rescue", "pending-tables"})
	if err != nil {
		return false, err
	}
	return hasPendingTableOutput(stdout), nil
}

func hasPendingTableOutput(stdout string) bool {
	trimmed := strings.TrimSpace(stdout)
	if trimmed == "" {
		return false
	}
	if strings.EqualFold(trimmed, "ok") {
		return false
	}
	return strings.Contains(trimmed, "mria_rlog_sync")
}

func filterRunningPods(items []corev1.Pod) []*corev1.Pod {
	var pods []*corev1.Pod
	for i := range items {
		pod := items[i]
		if pod.GetDeletionTimestamp() != nil {
			continue
		}
		if pod.Status.Phase != corev1.PodRunning {
			continue
		}
		pods = append(pods, pod.DeepCopy())
	}
	return pods
}

func podEligibleForRescue(pod *corev1.Pod, now time.Time) (bool, string) {
	if pod.Status.StartTime == nil {
		return false, "startTime is nil"
	}
	if pod.Status.StartTime.IsZero() {
		return false, "startTime is zero"
	}
	if pod.Status.Phase != corev1.PodRunning {
		return false, fmt.Sprintf("phase is %s", pod.Status.Phase)
	}
	if now.Sub(pod.Status.StartTime.Time) < clusterRescueMinPodAge {
		return false, fmt.Sprintf("pod age %s < %s", now.Sub(pod.Status.StartTime.Time).Round(time.Second), clusterRescueMinPodAge)
	}

	if len(pod.Status.ContainerStatuses) == 0 {
		return false, "no container statuses available"
	}
	emqxNotReady := false
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.State.Running == nil {
			return false, fmt.Sprintf("container %s state is not running", cs.Name)
		}
		if cs.State.Running.StartedAt.IsZero() {
			return false, fmt.Sprintf("container %s has zero startedAt timestamp", cs.Name)
		}
		if now.Sub(cs.State.Running.StartedAt.Time) < clusterRescueMinPodAge {
			return false, fmt.Sprintf("container %s started %s ago", cs.Name, now.Sub(cs.State.Running.StartedAt.Time).Round(time.Second))
		}
		if cs.RestartCount > 0 {
			if cs.LastTerminationState.Terminated == nil || cs.LastTerminationState.Terminated.FinishedAt.IsZero() {
				return false, fmt.Sprintf("container %s restarted without termination timestamp", cs.Name)
			}
			if now.Sub(cs.LastTerminationState.Terminated.FinishedAt.Time) < clusterRescueMinPodAge {
				return false, fmt.Sprintf("container %s restarted %s ago", cs.Name, now.Sub(cs.LastTerminationState.Terminated.FinishedAt.Time).Round(time.Second))
			}
		}
		if cs.Name == appsv2beta1.DefaultContainerName && !cs.Ready {
			emqxNotReady = true
		}
	}
	if !emqxNotReady {
		return false, fmt.Sprintf("%s container is ready", appsv2beta1.DefaultContainerName)
	}
	return true, ""
}

func selectRescueTarget(pods []*corev1.Pod) *corev1.Pod {
	sort.Slice(pods, func(i, j int) bool {
		if pods[i].CreationTimestamp.Equal(&pods[j].CreationTimestamp) {
			return pods[i].Name < pods[j].Name
		}
		return pods[i].CreationTimestamp.Before(&pods[j].CreationTimestamp)
	})
	return pods[0]
}
