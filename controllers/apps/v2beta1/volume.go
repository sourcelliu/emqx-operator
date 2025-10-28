package v2beta1

import (
	appsv2beta1 "github.com/emqx/emqx-operator/apis/apps/v2beta1"
	corev1 "k8s.io/api/core/v1"
)

// resolveVolumeSource returns the kube VolumeSource that should back the EMQX managed volume.
// Defaults to EmptyDir when no overriding configuration is provided.
func resolveVolumeSource(spec *appsv2beta1.VolumeSpec) corev1.VolumeSource {
	if spec == nil {
		return corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		}
	}
	if spec.HostPath != nil {
		return corev1.VolumeSource{
			HostPath: spec.HostPath.DeepCopy(),
		}
	}
	if spec.EmptyDir != nil {
		return corev1.VolumeSource{
			EmptyDir: spec.EmptyDir.DeepCopy(),
		}
	}
	return corev1.VolumeSource{
		EmptyDir: &corev1.EmptyDirVolumeSource{},
	}
}
