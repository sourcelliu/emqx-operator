package v1beta4

import (
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// +kubebuilder:object:generate=false
type Emqx interface {
	client.Object

	GetSpec() EmqxSpec
	GetStatus() EmqxStatus

	Default()
}
