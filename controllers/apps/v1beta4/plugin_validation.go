package v1beta4

import (
	"context"
	"reflect"

	appsv1beta4 "github.com/emqx/emqx-operator/apis/apps/v1beta4"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const pluginNameSnapshotAnnotation = "apps.emqx.io/v1beta4-plugin-name"

func (r *EmqxPluginReconciler) enforcePluginImmutability(ctx context.Context, instance *appsv1beta4.EmqxPlugin) error {
	original := instance.DeepCopy()

	annotations := instance.GetAnnotations()
	stored := ""
	if annotations != nil {
		stored = annotations[pluginNameSnapshotAnnotation]
	}

	if stored == "" {
		if annotations == nil {
			annotations = make(map[string]string)
		}
		annotations[pluginNameSnapshotAnnotation] = instance.Spec.PluginName
		instance.SetAnnotations(annotations)
	} else if stored != instance.Spec.PluginName {
		instance.Spec.PluginName = stored
	}

	if reflect.DeepEqual(original, instance) {
		return nil
	}

	return r.Client.Patch(ctx, instance, client.MergeFrom(original))
}
