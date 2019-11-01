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
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"

	"github.com/aryann/difflib"
	"github.com/sapcc/go-bits/logg"
	api_appsv1 "k8s.io/api/apps/v1"
	api_corev1 "k8s.io/api/core/v1"
	k8s_errors "k8s.io/apimachinery/pkg/api/errors"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
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

//ManagedObjectRef identifies a ManagedObject that's stored somewhere else.
type ManagedObjectRef struct {
	Kind      ObjectKind
	Name      string
	Namespace string
}

//ManagedObject describes the desired state of an object in k8s.
type ManagedObject struct {
	Kind      ObjectKind
	Name      string
	Namespace string
	//AccountName identifies the account that this object belongs to, if any.
	AccountName string
	//ApplyTo applies the desired attributes to a new or existing instance of
	//this object. The underlying type of the argument must match the ObjectKind.
	ApplyTo func(runtime.Object)
}

//Ref returns a ManagedObjectRef for this object.
func (mo ManagedObject) Ref() ManagedObjectRef {
	return ManagedObjectRef{mo.Kind, mo.Name, mo.Namespace}
}

//GetCurrentState returns the current state of this managed object on the k8s
//apiserver, or nil if it does not exist on the apiserver at the moment.
func (mo ManagedObject) GetCurrentState(info informers.SharedInformerFactory) (runtime.Object, error) {
	obj, err := mo.getCurrentState(info)
	if k8s_errors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		err = fmt.Errorf("cannot get %s %s: %s", mo.Kind, mo.Name, err.Error())
	}
	return obj, err
}

func (mo ManagedObject) getCurrentState(info informers.SharedInformerFactory) (runtime.Object, error) {
	switch mo.Kind {
	case ObjectKindConfigMap:
		lister := info.Core().V1().ConfigMaps().Lister()
		return lister.ConfigMaps(mo.Namespace).Get(mo.Name)
	case ObjectKindService:
		lister := info.Core().V1().Services().Lister()
		return lister.Services(mo.Namespace).Get(mo.Name)
	case ObjectKindDeployment:
		lister := info.Apps().V1().Deployments().Lister()
		return lister.Deployments(mo.Namespace).Get(mo.Name)
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
	logg.Debug("k8s worker: creating %s %s", mo.Kind, mo.Name)
	meta := meta_v1.ObjectMeta{
		Name:      mo.Name,
		Namespace: mo.Namespace,
	}
	cfg.AddCommonLabels(&meta.Labels)
	switch mo.Kind {
	case ObjectKindConfigMap:
		obj := &api_corev1.ConfigMap{ObjectMeta: meta}
		mo.ApplyTo(obj)
		return cfg.Clientset.CoreV1().ConfigMaps(mo.Namespace).Create(obj)
	case ObjectKindService:
		obj := &api_corev1.Service{ObjectMeta: meta}
		mo.ApplyTo(obj)
		return cfg.Clientset.CoreV1().Services(mo.Namespace).Create(obj)
	case ObjectKindDeployment:
		obj := &api_appsv1.Deployment{ObjectMeta: meta}
		mo.ApplyTo(obj)
		return cfg.Clientset.AppsV1().Deployments(mo.Namespace).Create(obj)
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
		logg.Debug("k8s worker: skipping update of %s %s: nothing to change", mo.Kind, mo.Name)
		return currentState, nil
	}

	logg.Debug("k8s worker: updating %s %s", mo.Kind, mo.Name)
	wantDiff, _ := strconv.ParseBool(os.Getenv("KEPPEL_DEBUG_KUBERNETES_DIFFS"))
	if wantDiff {
		err := mo.logDiff(desiredState, currentState)
		if err != nil {
			logg.Error("error while trying to print k8s object diff: " + err.Error())
		}
	}

	switch mo.Kind {
	case ObjectKindConfigMap:
		client := cfg.Clientset.CoreV1().ConfigMaps(mo.Namespace)
		return client.Update(desiredState.(*api_corev1.ConfigMap))
	case ObjectKindService:
		client := cfg.Clientset.CoreV1().Services(mo.Namespace)
		return client.Update(desiredState.(*api_corev1.Service))
	case ObjectKindDeployment:
		client := cfg.Clientset.AppsV1().Deployments(mo.Namespace)
		return client.Update(desiredState.(*api_appsv1.Deployment))
	default:
		panic(fmt.Sprintf("ManagedObject.Update() cannot handle kind %q", mo.Kind))
	}
}

////////////////////////////////////////////////////////////////////////////////
// helper for KEPPEL_DEBUG_KUBERNETES_DIFFS=true environment variable

func (mo ManagedObject) logDiff(desiredState, currentState runtime.Object) error {
	oldJSON, err := json.Marshal(currentState)
	if err != nil {
		return err
	}
	newJSON, err := json.Marshal(desiredState)
	if err != nil {
		return err
	}

	var (
		oldJSONBuf bytes.Buffer
		newJSONBuf bytes.Buffer
	)
	err = json.Indent(&oldJSONBuf, oldJSON, "", "  ")
	if err != nil {
		return err
	}
	err = json.Indent(&newJSONBuf, newJSON, "", "  ")
	if err != nil {
		return err
	}

	diffRecords := difflib.Diff(
		strings.SplitAfter(oldJSONBuf.String(), "\n"),
		strings.SplitAfter(newJSONBuf.String(), "\n"),
	)
	isNearDiff := make(map[int]bool, len(diffRecords)+6)
	for idx, record := range diffRecords {
		if record.Delta == difflib.Common {
			continue
		}
		for contextIdx := idx - 3; contextIdx <= idx+3; contextIdx++ {
			isNearDiff[contextIdx] = true
		}
	}
	if len(isNearDiff) == 0 {
		return fmt.Errorf("no diff detected for %s %s (why are we updating it!?)", mo.Kind, mo.Name)
	}

	//generate something resembling a unified diff
	logLines := make([]string, 0, len(diffRecords))
	countLinesOmitted := 0
	for idx, record := range diffRecords {
		if !isNearDiff[idx] {
			countLinesOmitted++
			continue
		}
		if countLinesOmitted > 0 {
			logLines = append(logLines,
				fmt.Sprintf("... %d lines omitted ...", countLinesOmitted),
			)
			countLinesOmitted = 0
		}
		logLines = append(logLines, strings.TrimSuffix(record.String(), "\n"))
	}
	logg.Debug("will apply the following diff for %s %s:\n%s", mo.Kind, mo.Name, strings.Join(logLines, "\n"))
	return nil
}
