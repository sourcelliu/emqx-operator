package v2beta1

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestHasPendingTableOutput(t *testing.T) {
	require.False(t, hasPendingTableOutput(""))
	require.False(t, hasPendingTableOutput("ok"))
	require.False(t, hasPendingTableOutput("OK"))
	require.True(t, hasPendingTableOutput("mnesia_table :: #{load_node => unknown}"))
	require.True(t, hasPendingTableOutput("\nmnesia_table :: #{load_node => unknown}\nok\n"))
}

func TestPodEligibleForRescue(t *testing.T) {
	now := time.Now()

	newPod := func(startDelta, containerDelta time.Duration) *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "emqx-0",
				CreationTimestamp: metav1.NewTime(now.Add(-time.Hour)),
			},
			Status: corev1.PodStatus{
				Phase:     corev1.PodRunning,
				StartTime: &metav1.Time{Time: now.Add(-startDelta)},
				ContainerStatuses: []corev1.ContainerStatus{
					{
						Name: "emqx",
						State: corev1.ContainerState{
							Running: &corev1.ContainerStateRunning{
								StartedAt: metav1.Time{Time: now.Add(-containerDelta)},
							},
						},
					},
				},
			},
		}
	}

	t.Run("too young pod start", func(t *testing.T) {
		pod := newPod(30*time.Second, 3*time.Minute)
		ok, reason := podEligibleForRescue(pod, now)
		require.False(t, ok)
		require.Contains(t, reason, "pod age")
	})

	t.Run("container not running long enough", func(t *testing.T) {
		pod := newPod(3*time.Minute, 30*time.Second)
		ok, reason := podEligibleForRescue(pod, now)
		require.False(t, ok)
		require.Contains(t, reason, "started")
	})

	t.Run("recent restart", func(t *testing.T) {
		pod := newPod(3*time.Minute, 3*time.Minute)
		pod.Status.ContainerStatuses[0].RestartCount = 1
		pod.Status.ContainerStatuses[0].LastTerminationState.Terminated = &corev1.ContainerStateTerminated{
			FinishedAt: metav1.Time{Time: now.Add(-time.Minute)},
		}
		ok, reason := podEligibleForRescue(pod, now)
		require.False(t, ok)
		require.Contains(t, reason, "restarted")
	})

	t.Run("eligible pod", func(t *testing.T) {
		pod := newPod(4*time.Minute, 4*time.Minute)
		pod.Status.ContainerStatuses[0].RestartCount = 1
		pod.Status.ContainerStatuses[0].LastTerminationState.Terminated = &corev1.ContainerStateTerminated{
			FinishedAt: metav1.Time{Time: now.Add(-4 * time.Minute)},
		}
		ok, reason := podEligibleForRescue(pod, now)
		require.True(t, ok, reason)
	})

	t.Run("ready container skips rescue", func(t *testing.T) {
		pod := newPod(4*time.Minute, 4*time.Minute)
		pod.Status.ContainerStatuses[0].Ready = true
		ok, reason := podEligibleForRescue(pod, now)
		require.False(t, ok)
		require.Contains(t, reason, "emqx")
	})
}
