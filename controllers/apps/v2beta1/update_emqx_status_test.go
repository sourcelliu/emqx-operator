package v2beta1

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"

	appsv2beta1 "github.com/emqx/emqx-operator/apis/apps/v2beta1"
	"github.com/emqx/emqx-operator/internal/handler"
	innerReq "github.com/emqx/emqx-operator/internal/requester"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestUpdateStatusKeepsNodesOnAPIFailure(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, appsv1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, appsv2beta1.AddToScheme(scheme))

	coreReplicas := int32(2)
	replicantReplicas := int32(1)
	instance := &appsv2beta1.EMQX{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "emqx",
			Namespace: "default",
		},
		Spec: appsv2beta1.EMQXSpec{
			Image: "emqx/emqx:latest",
			CoreTemplate: appsv2beta1.EMQXCoreTemplate{
				Spec: appsv2beta1.EMQXCoreTemplateSpec{
					EMQXReplicantTemplateSpec: appsv2beta1.EMQXReplicantTemplateSpec{
						Replicas: &coreReplicas,
					},
				},
			},
			ReplicantTemplate: &appsv2beta1.EMQXReplicantTemplate{
				Spec: appsv2beta1.EMQXReplicantTemplateSpec{
					Replicas: &replicantReplicas,
				},
			},
		},
		Status: appsv2beta1.EMQXStatus{
			CoreNodes: []appsv2beta1.EMQXNode{
				{
					Node:          "emqx@emqx-0",
					NodeStatus:    "running",
					ControllerUID: types.UID("core-sts"),
				},
			},
			CoreNodesStatus: appsv2beta1.EMQXNodesStatus{
				Replicas:        coreReplicas,
				ReadyReplicas:   coreReplicas,
				CurrentReplicas: coreReplicas,
				UpdateReplicas:  coreReplicas,
				CurrentRevision: "hash-core",
				UpdateRevision:  "hash-core",
			},
			ReplicantNodes: []appsv2beta1.EMQXNode{
				{
					Node:       "emqx@10.0.0.1",
					NodeStatus: "running",
				},
			},
			ReplicantNodesStatus: appsv2beta1.EMQXNodesStatus{
				Replicas:        replicantReplicas,
				ReadyReplicas:   replicantReplicas,
				CurrentReplicas: replicantReplicas,
				UpdateReplicas:  replicantReplicas,
				CurrentRevision: "hash-repl",
				UpdateRevision:  "hash-repl",
			},
			NodeEvacuationsStatus: []appsv2beta1.NodeEvacuationStatus{
				{Node: "emqx@emqx-0"},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(instance.DeepCopy()).
		WithStatusSubresource(&appsv2beta1.EMQX{}).
		Build()

	reconciler := &EMQXReconciler{
		Handler:       &handler.Handler{Client: fakeClient},
		EventRecorder: record.NewFakeRecorder(5),
	}

	updater := &updateStatus{EMQXReconciler: reconciler}
	requester := &innerReq.FakeRequester{
		ReqFunc: func(method string, url url.URL, body []byte, header http.Header) (*http.Response, []byte, error) {
			return nil, nil, errors.New("boom")
		},
	}

	live := &appsv2beta1.EMQX{}
	require.NoError(t, fakeClient.Get(context.Background(), types.NamespacedName{
		Name:      instance.Name,
		Namespace: instance.Namespace,
	}, live))

	origCoreNodes := append([]appsv2beta1.EMQXNode(nil), live.Status.CoreNodes...)
	origCoreStatus := live.Status.CoreNodesStatus
	origReplNodes := append([]appsv2beta1.EMQXNode(nil), live.Status.ReplicantNodes...)
	origReplStatus := live.Status.ReplicantNodesStatus
	origEvac := append([]appsv2beta1.NodeEvacuationStatus(nil), live.Status.NodeEvacuationsStatus...)

	res := updater.reconcile(context.Background(), logr.Discard(), live, requester)
	require.NoError(t, res.err)

	updated := &appsv2beta1.EMQX{}
	require.NoError(t, fakeClient.Get(context.Background(), types.NamespacedName{
		Name:      instance.Name,
		Namespace: instance.Namespace,
	}, updated))

	require.Equal(t, origCoreNodes, updated.Status.CoreNodes)
	require.Equal(t, origCoreStatus, updated.Status.CoreNodesStatus)
	require.Equal(t, origReplNodes, updated.Status.ReplicantNodes)
	require.Equal(t, origReplStatus, updated.Status.ReplicantNodesStatus)
	require.Equal(t, origEvac, updated.Status.NodeEvacuationsStatus)
}
