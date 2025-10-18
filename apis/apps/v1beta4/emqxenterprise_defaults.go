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
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var emqxenterpriselog = logf.Log.WithName("emqxenterprise-resource")

// Default populates missing fields on the resource with sensible values.
func (r *EmqxEnterprise) Default() {
	emqxenterpriselog.Info("default", "name", r.Name)

	defaultLabelsAndAnnotations(r)
	defaultEmqxImage(r)
	defaultEmqxACL(r)
	defaultEmqxConfig(r)
	defaultServiceTemplate(r)
	defaultContainerPort(r)
	defaultPersistent(r)
}
