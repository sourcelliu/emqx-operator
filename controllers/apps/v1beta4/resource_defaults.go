package v1beta4

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	appsv1beta4 "github.com/emqx/emqx-operator/apis/apps/v1beta4"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const emqxSpecSnapshotAnnotation = "apps.emqx.io/v1beta4-spec-snapshot"

type emqxSpecSnapshot struct {
	BootstrapAPIKeys  []appsv1beta4.BootstrapAPIKey         `json:"bootstrapAPIKeys,omitempty"`
	Persistent        *corev1.PersistentVolumeClaimTemplate `json:"persistent,omitempty"`
	PersistentPresent bool                                  `json:"persistentPresent,omitempty"`
	ClusterConfig     map[string]string                     `json:"clusterConfig,omitempty"`
}

func (r *EmqxReconciler) applyDefaultsAndValidation(ctx context.Context, instance appsv1beta4.Emqx) error {
	original := instance.DeepCopyObject().(client.Object)

	instance.Default()

	if err := r.validateAndSnapshot(instance); err != nil {
		return err
	}

	if instance.GetDeletionTimestamp() != nil {
		return nil
	}

	if reflect.DeepEqual(original, instance) {
		return nil
	}

	return r.Client.Patch(ctx, instance, client.MergeFrom(original))
}

func (r *EmqxReconciler) validateAndSnapshot(instance appsv1beta4.Emqx) error {
	if err := validateImageVersion(instance); err != nil {
		r.EventRecorder.Event(instance, corev1.EventTypeWarning, "InvalidSpec", err.Error())
		return err
	}

	annotations := instance.GetAnnotations()
	raw := ""
	if annotations != nil {
		raw = annotations[emqxSpecSnapshotAnnotation]
	}

	if raw != "" {
		var prev emqxSpecSnapshot
		if err := json.Unmarshal([]byte(raw), &prev); err != nil {
			return fmt.Errorf("failed to decode spec snapshot: %w", err)
		}
		r.restoreImmutableFields(instance, &prev)
	}

	snapshot := newSpecSnapshot(instance)
	data, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}

	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations[emqxSpecSnapshotAnnotation] = string(data)
	instance.SetAnnotations(annotations)

	return nil
}

func validateImageVersion(instance appsv1beta4.Emqx) error {
	version := instance.GetSpec().GetTemplate().Spec.EmqxContainer.Image.Version
	if version == "latest" {
		return fmt.Errorf("image version can not be latest")
	}

	v, err := semver.NewVersion(version)
	if err != nil {
		return fmt.Errorf("invalid image version: %s", version)
	}
	if v.Compare(semver.MustParse("4.4.14")) < 0 {
		return fmt.Errorf("image version %s is too old, please upgrade to 4.4.14 or later", version)
	}
	if v.Compare(semver.MustParse("5.0.0")) >= 0 {
		return fmt.Errorf("image version %s is too new, please downgrade to 5.0.0 earlier", version)
	}

	return nil
}

func (r *EmqxReconciler) restoreImmutableFields(instance appsv1beta4.Emqx, snapshot *emqxSpecSnapshot) {
	var (
		persistentWarnIssued bool
		bootstrapWarnIssued  bool
		clusterNameWarn      bool
		clusterPrefixWarn    bool
	)

	spec := instance.GetSpec()
	template := spec.GetTemplate()
	container := &template.Spec.EmqxContainer

	if snapshot.BootstrapAPIKeys != nil && !reflect.DeepEqual(snapshot.BootstrapAPIKeys, container.BootstrapAPIKeys) {
		container.BootstrapAPIKeys = append([]appsv1beta4.BootstrapAPIKey(nil), snapshot.BootstrapAPIKeys...)
		bootstrapWarnIssued = true
	}

	currentPersistent := spec.GetPersistent()
	switch {
	case snapshot.PersistentPresent:
		if currentPersistent == nil || !reflect.DeepEqual(snapshot.Persistent, currentPersistent) {
			spec.SetPersistent(snapshot.Persistent.DeepCopy())
			persistentWarnIssued = true
		}
	case !snapshot.PersistentPresent && currentPersistent != nil:
		spec.SetPersistent(nil)
		persistentWarnIssued = true
	}

	if len(snapshot.ClusterConfig) > 0 {
		config := container.EmqxConfig
		if config == nil {
			config = make(map[string]string)
		}

		for key, oldValue := range snapshot.ClusterConfig {
			if value, ok := config[key]; ok && value != oldValue {
				config[key] = oldValue
				if key == "name" {
					clusterNameWarn = true
				} else if strings.HasPrefix(key, "cluster") {
					clusterPrefixWarn = true
				}
			}
		}

		container.EmqxConfig = config
	}

	spec.SetTemplate(template)

	if bootstrapWarnIssued {
		r.EventRecorder.Event(instance, corev1.EventTypeWarning, "ImmutableField", "bootstrap APIKey cannot be updated")
	}

	if persistentWarnIssued {
		r.EventRecorder.Event(instance, corev1.EventTypeWarning, "ImmutableField", "refuse to update Persistent ")
	}

	if clusterNameWarn {
		r.EventRecorder.Event(instance, corev1.EventTypeWarning, "ImmutableField", `refuse to update the "name" field in ".spec.template.spec.emqxContainer.emqxConfig"`)
	}

	if clusterPrefixWarn {
		r.EventRecorder.Event(instance, corev1.EventTypeWarning, "ImmutableField", `refuse to update the "^cluster.*$" field in ".spec.template.spec.emqxContainer.emqxConfig"`)
	}
}

func newSpecSnapshot(instance appsv1beta4.Emqx) *emqxSpecSnapshot {
	spec := instance.GetSpec()
	template := spec.GetTemplate()
	container := template.Spec.EmqxContainer

	snapshot := &emqxSpecSnapshot{
		BootstrapAPIKeys: append([]appsv1beta4.BootstrapAPIKey(nil), container.BootstrapAPIKeys...),
		ClusterConfig:    extractClusterConfig(container.EmqxConfig),
	}

	if persistent := spec.GetPersistent(); persistent != nil {
		snapshot.Persistent = persistent.DeepCopy()
		snapshot.PersistentPresent = true
	}

	return snapshot
}

func extractClusterConfig(config map[string]string) map[string]string {
	if len(config) == 0 {
		return nil
	}

	result := make(map[string]string)
	for key, value := range config {
		if key == "name" || strings.HasPrefix(key, "cluster") {
			result[key] = value
		}
	}
	return result
}
