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
	"errors"
	"os"
	"strings"

	"github.com/sapcc/keppel/internal/keppel"
	api_corev1 "k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

//The base configuration file for keppel-registry, which contains all values
//that are shared among all keppel-registry processes. The remaining
//configuration options will be passed as environment variables in the
//Deployment's PodSpec.Env field.
const baseConfig = `
version: 0.1
log:
	accesslog:
		disabled: true
	level: info
http:
	addr: ':8080'
	relativeurls: true
	headers:
		X-Content-Type-Options: [nosniff]
health:
	storagedriver:
		enabled: true
		interval: 10s
		threshold: 3
storage:
	cache:
		blobdescriptor: inmemory
	delete:
		enabled: true
auth:
	token:
		rootcertbundle: /etc/keppel/issuer-cert-bundle.pem
`

//Configuration collects all configuration that is passed to this orchestration
//driver, either via NewOrchestrationDriver() or via environment variables.
type Configuration struct {
	NamespaceName string
	Marker        string
	RegistryImage string
	Keppel        keppel.Configuration
	StorageDriver keppel.StorageDriver
	Clientset     *kubernetes.Clientset
}

//NewConfiguration initializes the Configuration object.
func NewConfiguration(storage keppel.StorageDriver, keppelConfig keppel.Configuration) (*Configuration, error) {
	cfg := Configuration{
		NamespaceName: os.Getenv("KEPPEL_KUBERNETES_NAMESPACE"),
		Marker:        os.Getenv("KEPPEL_KUBERNETES_MARKER"),
		RegistryImage: os.Getenv("KEPPEL_REGISTRY_IMAGE"),
		StorageDriver: storage,
		Keppel:        keppelConfig,
	}
	if cfg.NamespaceName == "" {
		return nil, errors.New("missing environment variable: KEPPEL_KUBERNETES_NAMESPACE")
	}
	if cfg.Marker == "" {
		cfg.Marker = "registry"
	}
	if cfg.RegistryImage == "" {
		return nil, errors.New("missing environment variable: KEPPEL_REGISTRY_IMAGE")
	}

	clientConfig, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}
	cfg.Clientset, err = kubernetes.NewForConfig(clientConfig)
	if err != nil {
		return nil, err
	}

	return &cfg, nil
}

//AddCommonLabels adds the "heritage" and "marker" labels to the given
//ObjectMeta in-place. This orchestration driver uses these labels to recognize
//objects managed by it.
func (cfg Configuration) AddCommonLabels(labels *map[string]string) {
	if *labels == nil {
		*labels = make(map[string]string)
	}
	(*labels)["heritage"] = "keppel-api"
	(*labels)["marker"] = cfg.Marker
}

//CheckCommonLabels inspects the "heritage" and "marker" labels of the given
//ObjectMeta and returns whether this object is managed by this orchestration
//driver.
func (cfg Configuration) CheckCommonLabels(meta meta_v1.ObjectMeta) bool {
	return meta.Labels["heritage"] == "keppel-api" && meta.Labels["marker"] == cfg.Marker
}

//RenderConfigMap produces the ConfigMap that all keppel-registry deployments share.
//This ConfigMap will be mounted in all registry pods as /etc/keppel.
func (cfg Configuration) RenderConfigMap() ManagedObject {
	return ManagedObject{
		Kind: ObjectKindConfigMap,
		Name: cfg.Marker,
		ApplyTo: func(obj runtime.Object) {
			obj.(*api_corev1.ConfigMap).Data = map[string]string{
				//note to self: YAML does not allow tabs for indentation
				"registry-base.yaml":     strings.Replace(baseConfig, "\t", "    ", -1),
				"issuer-cert-bundle.pem": cfg.Keppel.JWTIssuerCertPEM,
			}
		},
	}
}
