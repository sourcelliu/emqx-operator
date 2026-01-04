package v2beta1

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"

	emperror "emperror.dev/errors"
	appsv2beta1 "github.com/emqx/emqx-operator/apis/apps/v2beta1"
	innerReq "github.com/emqx/emqx-operator/internal/requester"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type syncDashboardAdmin struct {
	*EMQXReconciler
}

func (s *syncDashboardAdmin) reconcile(ctx context.Context, logger logr.Logger, instance *appsv2beta1.EMQX, r innerReq.RequesterInterface) subResult {
	spec := instance.Spec.DashboardAdmin
	if spec == nil || spec.PasswordSecretRef == nil {
		if instance.Status.DashboardAdmin != nil {
			instance.Status.DashboardAdmin = nil
		}
		return subResult{}
	}

	username := spec.Username
	if username == "" {
		username = "admin"
	}

	if spec.PasswordSecretRef.Name == "" {
		return subResult{err: emperror.New("dashboardAdmin.passwordSecretRef.name must be set")}
	}
	if spec.PasswordSecretRef.Key == "" {
		return subResult{err: emperror.New("dashboardAdmin.passwordSecretRef.key must be set")}
	}

	passwordSecret := &corev1.Secret{}
	if err := s.Client.Get(ctx, types.NamespacedName{
		Namespace: instance.Namespace,
		Name:      spec.PasswordSecretRef.Name,
	}, passwordSecret); err != nil {
		return subResult{err: emperror.Wrap(err, "failed to get dashboard admin password secret")}
	}

	passwordBytes, ok := passwordSecret.Data[spec.PasswordSecretRef.Key]
	if !ok {
		return subResult{err: emperror.Errorf("secret %s/%s does not contain key %s", passwordSecret.Namespace, passwordSecret.Name, spec.PasswordSecretRef.Key)}
	}
	if len(passwordBytes) == 0 {
		return subResult{err: emperror.Errorf("secret %s/%s key %s is empty", passwordSecret.Namespace, passwordSecret.Name, spec.PasswordSecretRef.Key)}
	}

	digest := computeDashboardAdminDigest(username, passwordBytes, passwordSecret.ResourceVersion)
	if current := instance.Status.DashboardAdmin; current != nil {
		if current.Username == username &&
			current.PasswordHash == digest &&
			current.SecretResourceVersion == passwordSecret.ResourceVersion {
			return subResult{}
		}
	}

	if r == nil {
		return subResult{}
	}

	if !instance.Status.IsConditionTrue(appsv2beta1.CoreNodesReady) {
		return subResult{}
	}

	password := string(passwordBytes)
	if err := s.ensureDashboardAdmin(ctx, logger, instance, r, username, password, spec.CreateIfMissing); err != nil {
		return subResult{err: err}
	}

	if instance.Status.DashboardAdmin == nil {
		instance.Status.DashboardAdmin = &appsv2beta1.DashboardAdminStatus{}
	}
	instance.Status.DashboardAdmin.Username = username
	instance.Status.DashboardAdmin.PasswordHash = digest
	instance.Status.DashboardAdmin.SecretResourceVersion = passwordSecret.ResourceVersion
	instance.Status.DashboardAdmin.LastSynced = metav1.Now()

	s.EventRecorder.Event(instance, corev1.EventTypeNormal, "DashboardAdminPasswordReset", "Dashboard admin credentials reconciled")

	return subResult{}
}

func (s *syncDashboardAdmin) ensureDashboardAdmin(ctx context.Context, logger logr.Logger, instance *appsv2beta1.EMQX, r innerReq.RequesterInterface, username, password string, createIfMissing bool) error {
	return s.syncDashboardAdminWithCtl(ctx, logger, instance, username, password, createIfMissing)
}

func (s *syncDashboardAdmin) syncDashboardAdminWithCtl(ctx context.Context, logger logr.Logger, instance *appsv2beta1.EMQX, username, password string, createIfMissing bool) error {
	pod, err := s.pickReadyCorePod(ctx, instance)
	if err != nil {
		return emperror.Wrap(err, "failed to select ready EMQX pod for password reset")
	}
	if pod == nil {
		return emperror.Errorf("no ready EMQX core pod available for password reset")
	}
	cmd := []string{"emqx_ctl", "admins", "passwd", username, password}
	if err := s.execEmqxCommand(ctx, logger, pod, cmd); err != nil {
		return emperror.Wrap(err, "failed to reset dashboard admin via emqx_ctl")
	}
	return nil
}

func (s *syncDashboardAdmin) pickReadyCorePod(ctx context.Context, instance *appsv2beta1.EMQX) (*corev1.Pod, error) {
	podList := &corev1.PodList{}
	if err := s.Client.List(ctx, podList,
		client.InNamespace(instance.Namespace),
		client.MatchingLabels(appsv2beta1.DefaultCoreLabels(instance)),
	); err != nil {
		return nil, err
	}
	if len(podList.Items) == 0 {
		return nil, nil
	}

	sort.Slice(podList.Items, func(i, j int) bool {
		return podList.Items[i].CreationTimestamp.Before(&podList.Items[j].CreationTimestamp)
	})

	for _, pod := range podList.Items {
		if pod.GetDeletionTimestamp() != nil {
			continue
		}
		for _, cond := range pod.Status.Conditions {
			if cond.Type == corev1.ContainersReady && cond.Status == corev1.ConditionTrue {
				copy := pod.DeepCopy()
				return copy, nil
			}
		}
	}
	return nil, nil
}

func computeDashboardAdminDigest(username string, password []byte, resourceVersion string) string {
	sum := sha256.Sum256([]byte(username + ":" + string(password) + ":" + resourceVersion))
	return hex.EncodeToString(sum[:])
}

func syncDashboardAdminByAPI(r innerReq.RequesterInterface, username, password string, createIfMissing bool) error {
	if r == nil {
		return emperror.New("requester is nil")
	}

	payload, _ := json.Marshal(map[string]string{
		"password": password,
	})
	updateURL := r.GetURL(fmt.Sprintf("api/v5/dashboards/admins/%s", username))
	resp, body, err := r.Request(http.MethodPut, updateURL, payload, nil)
	if err != nil {
		return emperror.Wrap(err, "failed to update dashboard admin password")
	}

	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated:
		return nil
	case http.StatusNotFound:
		if !createIfMissing {
			return emperror.Errorf("dashboard admin %s not found", username)
		}
	default:
		return emperror.Errorf("failed to reset dashboard admin password, status : %s, body: %s", resp.Status, string(body))
	}

	createPayload, _ := json.Marshal(map[string]string{
		"username": username,
		"password": password,
	})
	createURL := r.GetURL("api/v5/dashboards/admins")
	resp, body, err = r.Request(http.MethodPost, createURL, createPayload, nil)
	if err != nil {
		return emperror.Wrap(err, "failed to create dashboard admin user")
	}
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return emperror.Errorf("failed to create dashboard admin user, status : %s, body: %s", resp.Status, string(body))
	}
	return nil
}
