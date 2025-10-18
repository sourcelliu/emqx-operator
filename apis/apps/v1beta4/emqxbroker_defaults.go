/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1beta4

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// log is for logging in this package.
var emqxbrokerlog = logf.Log.WithName("emqxbroker-resource")

// Default populates missing fields on the resource with sensible values.
func (r *EmqxBroker) Default() {
	emqxbrokerlog.Info("default", "name", r.Name)

	defaultLabelsAndAnnotations(r)
	defaultEmqxImage(r)
	defaultEmqxACL(r)
	defaultEmqxConfig(r)
	defaultServiceTemplate(r)
	defaultContainerPort(r)
	defaultPersistent(r)
}

func defaultLabelsAndAnnotations(r Emqx) {
	labels := r.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}

	labels["apps.emqx.io/managed-by"] = "emqx-operator"
	labels["apps.emqx.io/instance"] = r.GetName()
	r.SetLabels(labels)

	template := r.GetSpec().GetTemplate()
	template.Labels = mergeMap(template.Labels, labels)
	template.Annotations = mergeMap(template.Annotations, r.GetAnnotations())
	delete(template.Annotations, "kubectl.kubernetes.io/last-applied-configuration")

	r.GetSpec().SetTemplate(template)
}

func defaultEmqxImage(r Emqx) {
	template := r.GetSpec().GetTemplate()
	if template.Spec.EmqxContainer.Image.Repository == "" {
		if _, ok := r.(*EmqxBroker); ok {
			template.Spec.EmqxContainer.Image.Repository = "emqx/emqx"
		}
		if _, ok := r.(*EmqxEnterprise); ok {
			template.Spec.EmqxContainer.Image.Repository = "emqx/emqx-ee"
		}
	}
	r.GetSpec().SetTemplate(template)
}

func defaultEmqxACL(r Emqx) {
	template := r.GetSpec().GetTemplate()
	if len(template.Spec.EmqxContainer.EmqxACL) == 0 {
		template.Spec.EmqxContainer.EmqxACL = []string{
			`{allow, {user, "dashboard"}, subscribe, ["$SYS/#"]}.`,
			`{allow, {ipaddr, "127.0.0.1"}, pubsub, ["$SYS/#", "#"]}.`,
			`{deny, all, subscribe, ["$SYS/#", {eq, "#"}]}.`,
			`{allow, all}.`,
		}
	}
	r.GetSpec().SetTemplate(template)
}

func defaultEmqxConfig(r Emqx) {
	names := &Names{r}

	template := r.GetSpec().GetTemplate()
	if template.Spec.EmqxContainer.EmqxConfig == nil {
		template.Spec.EmqxContainer.EmqxConfig = make(map[string]string)
	}

	clusterConfig := make(map[string]string)
	clusterConfig["name"] = r.GetName()
	clusterConfig["log.to"] = "console"
	clusterConfig["cluster.discovery"] = "dns"
	clusterConfig["cluster.dns.type"] = "srv"
	clusterConfig["cluster.dns.app"] = r.GetName()
	clusterConfig["cluster.dns.name"] = fmt.Sprintf("%s.%s.svc.%s", names.HeadlessSvc(), r.GetNamespace(), r.GetSpec().GetClusterDomain())

	clusterConfig["listener.tcp.internal"] = ""
	for k, v := range clusterConfig {
		if _, ok := template.Spec.EmqxContainer.EmqxConfig[k]; !ok {
			template.Spec.EmqxContainer.EmqxConfig[k] = v
		}
	}
	r.GetSpec().SetTemplate(template)
}

func defaultServiceTemplate(r Emqx) {
	s := r.GetSpec().GetServiceTemplate()

	if s.Name == "" {
		s.Name = r.GetName()
	}
	s.Namespace = r.GetNamespace()
	s.Labels = mergeMap(s.Labels, r.GetLabels())
	s.Annotations = mergeMap(s.ObjectMeta.Annotations, r.GetAnnotations())
	delete(s.Annotations, "kubectl.kubernetes.io/last-applied-configuration")

	s.Spec.Selector = r.GetLabels()
	s.Spec.Ports = MergeServicePorts(
		s.Spec.Ports,
		[]corev1.ServicePort{
			{
				Name:       "http-management-8081",
				Port:       8081,
				Protocol:   corev1.ProtocolTCP,
				TargetPort: intstr.FromInt(8081),
			},
			{
				Name:       "http-dashboard-18083",
				Port:       18083,
				Protocol:   corev1.ProtocolTCP,
				TargetPort: intstr.FromInt(18083),
			},
		},
	)

	r.GetSpec().SetServiceTemplate(s)
}

func defaultContainerPort(r Emqx) {
	temp := r.GetSpec().GetTemplate()
	container := &temp.Spec.EmqxContainer
	container.Ports = MergeContainerPorts(
		container.Ports,
		[]corev1.ContainerPort{
			{
				Name:          "management",
				Protocol:      corev1.ProtocolTCP,
				ContainerPort: 8081,
			},
			{
				Name:          "dashboard",
				Protocol:      corev1.ProtocolTCP,
				ContainerPort: 18083,
			},
		},
	)

	r.GetSpec().SetTemplate(temp)
}

func defaultPersistent(r Emqx) {
	p := r.GetSpec().GetPersistent()
	if p == nil {
		return
	}

	if p.Name == "" {
		names := Names{Object: r}
		p.Name = names.Data()
	}
	p.Namespace = r.GetNamespace()
	p.Labels = mergeMap(p.Labels, r.GetLabels())
	p.Annotations = mergeMap(p.Annotations, r.GetAnnotations())
	delete(p.Annotations, "kubectl.kubernetes.io/last-applied-configuration")
	r.GetSpec().SetPersistent(p)
}
