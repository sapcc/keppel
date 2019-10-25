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
	"fmt"
	"reflect"

	api_appsv1 "k8s.io/api/apps/v1"
	api_corev1 "k8s.io/api/core/v1"
	k8s_errors "k8s.io/apimachinery/pkg/api/errors"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

//ObjectKind is an enum containing the kinds that are supported by type
//ManagedObject.
type ObjectKind string

const (
	//ObjectKindConfigMap is a kind supported by ManagedObject.
	ObjectKindConfigMap ObjectKind = "ConfigMap"
	//ObjectKindService is a kind supported by ManagedObject.
	ObjectKindService ObjectKind = "Service"
	//ObjectKindDeployment is a kind supported by ManagedObject.
	ObjectKindDeployment ObjectKind = "Deployment"
)

//IdentifyObject returns the kind and ObjectMeta of the given object.
func IdentifyObject(obj runtime.Object) (ObjectKind, meta_v1.ObjectMeta) {
	switch obj := obj.(type) {
	case *api_corev1.ConfigMap:
		return ObjectKindConfigMap, obj.ObjectMeta
	case *api_corev1.Service:
		return ObjectKindService, obj.ObjectMeta
	case *api_appsv1.Deployment:
		return ObjectKindDeployment, obj.ObjectMeta
	default:
		panic(fmt.Sprintf("Identify() cannot handle type %T", obj))
	}
}

//ManagedObject describes the desired state of an object in k8s.
type ManagedObject struct {
	Kind ObjectKind
	Name string
	//AccountName identifies the account that this object belongs to, if any.
	AccountName string
	//ApplyTo applies the desired attributes to a new or existing instance of
	//this object. The underlying type of the argument must match the ObjectKind.
	ApplyTo func(runtime.Object)
}

//GetCurrentState returns the current state of this managed object on the k8s
//apiserver, or nil if it does not exist on the apiserver at the moment.
func (mo ManagedObject) GetCurrentState(cfg *Configuration) (runtime.Object, error) {
	obj, err := mo.getCurrentState(cfg)
	if k8s_errors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		err = fmt.Errorf("cannot get %s %s: %s", mo.Kind, mo.Name, err.Error())
	}
	return obj, err
}

func (mo ManagedObject) getCurrentState(cfg *Configuration) (runtime.Object, error) {
	switch mo.Kind {
	case ObjectKindConfigMap:
		client := cfg.Clientset.CoreV1().ConfigMaps(cfg.NamespaceName)
		return client.Get(mo.Name, meta_v1.GetOptions{})
	case ObjectKindService:
		client := cfg.Clientset.CoreV1().Services(cfg.NamespaceName)
		return client.Get(mo.Name, meta_v1.GetOptions{})
	case ObjectKindDeployment:
		client := cfg.Clientset.AppsV1().Deployments(cfg.NamespaceName)
		return client.Get(mo.Name, meta_v1.GetOptions{})
	default:
		panic(fmt.Sprintf("ManagedObject.GetCurrentState() cannot handle kind %q", mo.Kind))
	}
}

//CreateOrUpdate calls either Create or Update depending on whether
//`currentState` is nil or not.
func (mo ManagedObject) CreateOrUpdate(currentState runtime.Object, cfg *Configuration) (runtime.Object, error) {
	if currentState == nil {
		newState, err := mo.Create(cfg)
		if err != nil {
			err = fmt.Errorf("cannot create %s %s: %s", mo.Kind, mo.Name, err.Error())
		}
		return newState, err
	}
	newState, err := mo.Update(currentState, cfg)
	if err != nil {
		err = fmt.Errorf("cannot update %s %s: %s", mo.Kind, mo.Name, err.Error())
	}
	return newState, err
}

//Create attempts to create this ManagedObject on the server. On success,
//returns the state of the object as returned by the server.
func (mo ManagedObject) Create(cfg *Configuration) (runtime.Object, error) {
	meta := meta_v1.ObjectMeta{
		Name:      mo.Name,
		Namespace: cfg.NamespaceName,
	}
	cfg.AddCommonLabels(&meta.Labels)
	switch mo.Kind {
	case ObjectKindConfigMap:
		obj := &api_corev1.ConfigMap{ObjectMeta: meta}
		mo.ApplyTo(obj)
		return cfg.Clientset.CoreV1().ConfigMaps(cfg.NamespaceName).Create(obj)
	case ObjectKindService:
		obj := &api_corev1.Service{ObjectMeta: meta}
		mo.ApplyTo(obj)
		return cfg.Clientset.CoreV1().Services(cfg.NamespaceName).Create(obj)
	case ObjectKindDeployment:
		obj := &api_appsv1.Deployment{ObjectMeta: meta}
		mo.ApplyTo(obj)
		return cfg.Clientset.AppsV1().Deployments(cfg.NamespaceName).Create(obj)
	default:
		panic(fmt.Sprintf("ManagedObject.Create() cannot handle kind %q", mo.Kind))
	}
}

//Update attempts to update this ManagedObject on the server if any changes are
//deemed necessary. On success, returns the state of the object as returned by
//the server.
func (mo ManagedObject) Update(currentState runtime.Object, cfg *Configuration) (runtime.Object, error) {
	//only do an update if we really need to change something
	desiredState := currentState.DeepCopyObject()
	mo.ApplyTo(desiredState)
	if reflect.DeepEqual(desiredState, currentState) {
		return currentState, nil
	}

	switch mo.Kind {
	case ObjectKindConfigMap:
		client := cfg.Clientset.CoreV1().ConfigMaps(cfg.NamespaceName)
		return client.Update(desiredState.(*api_corev1.ConfigMap))
	case ObjectKindService:
		client := cfg.Clientset.CoreV1().Services(cfg.NamespaceName)
		return client.Update(desiredState.(*api_corev1.Service))
	case ObjectKindDeployment:
		client := cfg.Clientset.AppsV1().Deployments(cfg.NamespaceName)
		return client.Update(desiredState.(*api_appsv1.Deployment))
	default:
		panic(fmt.Sprintf("ManagedObject.Update() cannot handle kind %q", mo.Kind))
	}
}
