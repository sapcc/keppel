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
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/orchestration"
	api_appsv1 "k8s.io/api/apps/v1"
	api_corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"
)

func init() {
	keppel.RegisterOrchestrationDriver("kubernetes", func(storage keppel.StorageDriver, keppelConfig keppel.Configuration, db keppel.DBAccessForOrchestrationDriver) (keppel.OrchestrationDriver, error) {
		cfg, err := NewConfiguration(storage, keppelConfig)
		return &orchestration.Engine{
			Launcher: &driver{Config: cfg},
			DB:       db,
		}, err
	})
}

type driver struct {
	Config *Configuration
	//fields that are initialized by .Init()
	AddChan          chan<- ManagedObject
	ConnectivityChan chan<- orchestration.RegistryConnectivityMessage
}

//Init implements the orchestration.RegistryLauncher interface.
func (d *driver) Init(processCtx context.Context, wg *sync.WaitGroup, connectivityChan chan<- orchestration.RegistryConnectivityMessage) {
	addChan := make(chan ManagedObject, 5)
	addChan <- d.Config.RenderConfigMap()
	d.AddChan = addChan
	d.ConnectivityChan = connectivityChan

	//TODO pass processCtx and wg
	go runDriverMainLoop(d.Config, addChan, connectivityChan)
}

//LaunchRegistry implements the orchestration.RegistryLauncher interface.
func (d *driver) LaunchRegistry(accountCtx context.Context, account keppel.Account) {
	//TODO react to expiry of `accountCtx`
	objectName := fmt.Sprintf(`%s-%s`, d.Config.Marker, account.Name)

	d.AddChan <- ManagedObject{
		Kind:        ObjectKindService,
		Name:        objectName,
		AccountName: account.Name,
		ApplyTo: func(obj runtime.Object) {
			svc := obj.(*api_corev1.Service)
			svc.Spec.Ports = []api_corev1.ServicePort{{
				Name:     "http",
				Protocol: api_corev1.ProtocolTCP,
				Port:     8080,
			}}
			svc.Spec.Selector = map[string]string{"name": objectName}
			svc.Spec.Type = api_corev1.ServiceTypeClusterIP
		},
	}

	d.AddChan <- ManagedObject{
		Kind:        ObjectKindDeployment,
		Name:        objectName,
		AccountName: account.Name,
		ApplyTo: func(obj runtime.Object) {
			depl := obj.(*api_appsv1.Deployment)
			setupDeployment(depl, d.Config, account)
		},
	}
}

func p2int32(val int32) *int32 {
	return &val
}
func p2intstr(val int32) *intstr.IntOrString {
	return &intstr.IntOrString{Type: intstr.Int, IntVal: val}
}

type objectChangeMessage struct {
	Kind  ObjectKind
	Name  string
	State runtime.Object //= nil for deleted objects
}

//Sets up informers for all the object types that we're interested in.
//Add/update/delete notifications are all funneled into a single channel that
//is consumed by the driver main loop below.
func setupInformers(cfg *Configuration) <-chan objectChangeMessage {
	notifyChan := make(chan objectChangeMessage, 10)
	sendChange := func(obj, state runtime.Object) {
		kind, objectMeta := IdentifyObject(obj)
		if cfg.CheckCommonLabels(objectMeta) {
			notifyChan <- objectChangeMessage{
				Kind:  kind,
				Name:  objectMeta.Name,
				State: state,
			}
		}
	}

	funcs := cache.ResourceEventHandlerFuncs{
		AddFunc: func(objUntyped interface{}) {
			obj := objUntyped.(runtime.Object)
			sendChange(obj, obj)
		},
		UpdateFunc: func(oldObjUntyped, newObjUntyped interface{}) {
			oldObj := oldObjUntyped.(runtime.Object)
			newObj := newObjUntyped.(runtime.Object)
			sendChange(oldObj, newObj)
		},
		DeleteFunc: func(objUntyped interface{}) {
			obj := objUntyped.(runtime.Object)
			sendChange(obj, nil)
		},
	}

	informerFactory := informers.NewSharedInformerFactoryWithOptions(
		cfg.Clientset, 30*time.Second,
		informers.WithNamespace(cfg.NamespaceName), //only watch this namespace
	)
	informerFactory.Core().V1().ConfigMaps().Informer().AddEventHandler(funcs)
	informerFactory.Core().V1().Services().Informer().AddEventHandler(funcs)
	informerFactory.Apps().V1().Deployments().Informer().AddEventHandler(funcs)
	informerFactory.Start(wait.NeverStop)

	return notifyChan
}

func runDriverMainLoop(cfg *Configuration, addChan <-chan ManagedObject, connectivityChan chan<- orchestration.RegistryConnectivityMessage) {
	notifyChan := setupInformers(cfg)
	managedObjects := map[ObjectKind]map[string]ManagedObject{
		ObjectKindConfigMap:  {},
		ObjectKindService:    {},
		ObjectKindDeployment: {},
	}

	for {
		select {
		case mo := <-addChan:
			logg.Debug("adding %s %s to managed objects", mo.Kind, mo.Name)
			managedObjects[mo.Kind][mo.Name] = mo

			var newState runtime.Object
			currentState, err := mo.GetCurrentState(cfg)
			if err == nil {
				newState, err = mo.CreateOrUpdate(currentState, cfg)
			}
			if err != nil {
				if mo.AccountName != "" {
					connectivityChan <- orchestration.RegistryConnectivityMessage{
						AccountName: mo.AccountName,
						Err:         err,
					}
				} else {
					logg.Error(err.Error())
				}
				continue
			}

			//for pre-existing services, CreateOrUpdate() might be a no-op and we
			//might not see an update notification, so this might be our only
			//chance to send a connectivity message
			maybePublishServiceIP(mo, newState, connectivityChan)

		case msg := <-notifyChan:
			mo, exists := managedObjects[msg.Kind][msg.Name]
			if !exists {
				logg.Debug("ignoring change on unmanaged %s %s", msg.Kind, msg.Name)
				continue
			}
			newState, err := mo.CreateOrUpdate(msg.State, cfg)
			if err != nil {
				logg.Error(err.Error())
			}
			maybePublishServiceIP(mo, newState, connectivityChan)
		}
	}
}

func maybePublishServiceIP(mo ManagedObject, state runtime.Object, connectivityChan chan<- orchestration.RegistryConnectivityMessage) {
	if mo.Kind != "Service" {
		return
	}
	svc := state.(*api_corev1.Service)
	if svc.Spec.ClusterIP != "" {
		connectivityChan <- orchestration.RegistryConnectivityMessage{
			AccountName: mo.AccountName,
			Host:        svc.Spec.ClusterIP + ":8080",
		}
	}
}
