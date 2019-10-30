/******************************************************************************
*
*  Copyright 2019 SAP SE
*
*  Licensed under the Apache License, Version 2.0 (the "License");
*  you may not use this file except in compliance with the License.
*  You may obtain a copy of the License at
*
*      http://www.apache.org/licenses/LICENSE-2.0
*
*  Unless required by applicable law or agreed to in writing, software
*  distributed under the License is distributed on an "AS IS" BASIS,
*  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
*  See the License for the specific language governing permissions and
*  limitations under the License.
*
******************************************************************************/

package kubernetesdriver

import (
	"sort"

	"github.com/sapcc/keppel/internal/keppel"
	api_appsv1 "k8s.io/api/apps/v1"
	api_corev1 "k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func setupDeployment(depl *api_appsv1.Deployment, cfg *Configuration, account keppel.Account) {
	setupDeploymentStrategy(depl)
	setupDeploymentPodLabels(depl, cfg)
	setupDeploymentPodSpec(depl, cfg, account)
}

func setupDeploymentStrategy(depl *api_appsv1.Deployment) {
	if depl.Spec.Replicas == nil || *depl.Spec.Replicas != 2 {
		//TODO make replica count configurable
		depl.Spec.Replicas = p2int32(2)
	}
	depl.Spec.Strategy.Type = api_appsv1.RollingUpdateDeploymentStrategyType
	depl.Spec.Strategy.RollingUpdate = &api_appsv1.RollingUpdateDeployment{
		MaxUnavailable: p2intstr(0),
		MaxSurge:       p2intstr(1),
	}
	depl.Spec.MinReadySeconds = 10
}

func setupDeploymentPodLabels(depl *api_appsv1.Deployment, cfg *Configuration) {
	depl.Spec.Selector = &meta_v1.LabelSelector{}

	cfg.AddCommonLabels(&depl.Spec.Selector.MatchLabels)
	cfg.AddCommonLabels(&depl.Spec.Template.ObjectMeta.Labels)

	depl.Spec.Selector.MatchLabels["name"] = depl.ObjectMeta.Name
	depl.Spec.Template.ObjectMeta.Labels["name"] = depl.ObjectMeta.Name
}

func setupDeploymentPodSpec(depl *api_appsv1.Deployment, cfg *Configuration, account keppel.Account) {
	spec := &depl.Spec.Template.Spec

	spec.Volumes = []api_corev1.Volume{{
		Name: "keppel-etc",
		VolumeSource: api_corev1.VolumeSource{
			ConfigMap: &api_corev1.ConfigMapVolumeSource{
				LocalObjectReference: api_corev1.LocalObjectReference{
					Name: cfg.Marker,
				},
			},
		},
	}}

	var container api_corev1.Container
	for _, cont := range spec.Containers {
		if cont.Name == "registry" {
			container = cont
			break
		}
	}
	setupDeploymentContainerSpec(&container, cfg, account)
	spec.Containers = []api_corev1.Container{container}
}

func setupDeploymentContainerSpec(cont *api_corev1.Container, cfg *Configuration, account keppel.Account) {
	cont.Name = "registry"
	cont.Image = cfg.RegistryImage
	cont.Command = []string{"keppel-registry"}
	cont.Args = []string{"serve", "/etc/keppel/registry-base.yaml"}

	cont.Ports = []api_corev1.ContainerPort{{
		Name:          "http",
		ContainerPort: 8080,
		Protocol:      api_corev1.ProtocolTCP,
	}}

	envVars := cfg.StorageDriver.GetEnvironment(account)
	for k, v := range cfg.Keppel.ToRegistryEnvironment() {
		envVars[k] = v
	}
	cont.Env = updateEnvListFromMap(cont.Env, envVars)

	cont.VolumeMounts = []api_corev1.VolumeMount{{
		Name:      "keppel-etc",
		ReadOnly:  true,
		MountPath: "/etc/keppel",
	}}

	//TODO liveness/readiness probes
	//TODO resource requirements?
}

func updateEnvListFromMap(oldVars []api_corev1.EnvVar, newVars map[string]string) []api_corev1.EnvVar {
	//sort variable names to make `kubectl get pod -f yaml` look nice
	names := make([]string, 0, len(oldVars)+len(newVars))
	mergedVars := make(map[string]api_corev1.EnvVar)
	for _, variable := range oldVars {
		names = append(names, variable.Name)
		mergedVars[variable.Name] = variable
	}
	for name, value := range newVars {
		if _, exists := mergedVars[name]; !exists {
			names = append(names, name)
		}
		mergedVars[name] = api_corev1.EnvVar{Name: name, Value: value}
	}
	sort.Strings(names)

	result := make([]api_corev1.EnvVar, len(names))
	for idx, name := range names {
		result[idx] = mergedVars[name]
	}
	return result
}

func p2int32(val int32) *int32 {
	return &val
}
func p2intstr(val int32) *intstr.IntOrString {
	return &intstr.IntOrString{Type: intstr.Int, IntVal: val}
}
