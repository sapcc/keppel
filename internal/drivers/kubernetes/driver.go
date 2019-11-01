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
	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

//TODO remove unknown objects after grace period
//TODO skip error logs when update fails because of conflict

func init() {
	keppel.RegisterOrchestrationDriver("kubernetes", func(storage keppel.StorageDriver, keppelConfig keppel.Configuration, db keppel.DBAccessForOrchestrationDriver) (keppel.OrchestrationDriver, error) {
		cfg, err := NewConfiguration(storage, keppelConfig)
		return &orchestration.Engine{
			Launcher: &driver{
				Config:              cfg,
				ManagedObjects:      map[ManagedObjectRef]ManagedObject{},
				ManagedObjectsMutex: &sync.RWMutex{},
				ProcessingQueue: workqueue.NewRateLimitingQueue(
					workqueue.DefaultControllerRateLimiter(),
				),
			},
			DB: db,
		}, err
	})
}

type driver struct {
	Config              *Configuration
	ManagedObjects      map[ManagedObjectRef]ManagedObject
	ManagedObjectsMutex *sync.RWMutex
	ProcessingQueue     workqueue.RateLimitingInterface
	//fields that are initialized by .Init()
	ConnectivityChan chan<- orchestration.RegistryConnectivityMessage
	//fields that are initialized by .run()
	Informers informers.SharedInformerFactory
}

//Init implements the orchestration.RegistryLauncher interface.
func (d *driver) Init(ctx context.Context, wg *sync.WaitGroup, connectivityChan chan<- orchestration.RegistryConnectivityMessage, allAccounts []keppel.Account) {
	d.ConnectivityChan = connectivityChan

	//SAFETY: These calls are safe without mutex lock because no other goroutines
	//are accessing d.ManagedObjects yet.
	for _, account := range allAccounts {
		d.addManagedObjectsFor(account)
	}
	d.addManagedObject(d.Config.RenderConfigMap())

	go d.run(ctx, wg)
}

//LaunchRegistry implements the orchestration.RegistryLauncher interface.
func (d *driver) LaunchRegistry(account keppel.Account) {
	d.ManagedObjectsMutex.Lock()
	defer d.ManagedObjectsMutex.Unlock()
	d.addManagedObjectsFor(account)
}

func (d *driver) addManagedObjectsFor(account keppel.Account) {
	//NOTE: This method assumes that d.ManagedObjectsMutex is already locked!
	objectName := fmt.Sprintf(`%s-%s`, d.Config.Marker, account.Name)

	d.addManagedObject(ManagedObject{
		Kind:        ObjectKindService,
		Name:        objectName,
		Namespace:   d.Config.NamespaceName,
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
	})

	d.addManagedObject(ManagedObject{
		Kind:        ObjectKindDeployment,
		Name:        objectName,
		Namespace:   d.Config.NamespaceName,
		AccountName: account.Name,
		ApplyTo: func(obj runtime.Object) {
			depl := obj.(*api_appsv1.Deployment)
			setupDeployment(depl, d.Config, account)
		},
	})
}

func (d *driver) addManagedObject(mo ManagedObject) {
	//NOTE: This method assumes that d.ManagedObjectsMutex is already locked!
	logg.Debug("adding %s %s to managed objects", mo.Kind, mo.Name)
	ref := mo.Ref()
	d.ManagedObjects[ref] = mo

	//add to queue immediately to converge towards the desired state (e.g. to
	//create the object if it does not exist in k8s yet)
	d.ProcessingQueue.Add(ref)
}

type objectChangeMessage struct {
	Kind  ObjectKind
	Name  string
	State runtime.Object //= nil for deleted objects
}

//Number of worker goroutines.
const threadiness = 5

func (d *driver) run(ctx context.Context, wg *sync.WaitGroup) {
	wg.Add(1)
	defer wg.Done()

	enqueueObject := func(obj interface{}) {
		kind, objectMeta := IdentifyObject(obj.(runtime.Object))
		if d.Config.CheckCommonLabels(objectMeta) {
			d.ProcessingQueue.Add(ManagedObjectRef{
				Kind:      kind,
				Name:      objectMeta.Name,
				Namespace: objectMeta.Namespace,
			})
		}
	}
	funcs := cache.ResourceEventHandlerFuncs{
		AddFunc:    enqueueObject,
		UpdateFunc: func(_, obj interface{}) { enqueueObject(obj) },
		DeleteFunc: enqueueObject,
	}

	d.Informers = informers.NewSharedInformerFactoryWithOptions(
		d.Config.Clientset, 30*time.Second,
		informers.WithNamespace(d.Config.NamespaceName), //only watch this namespace
	)
	d.Informers.Core().V1().ConfigMaps().Informer().AddEventHandler(funcs)
	d.Informers.Core().V1().Services().Informer().AddEventHandler(funcs)
	d.Informers.Apps().V1().Deployments().Informer().AddEventHandler(funcs)
	d.Informers.Start(ctx.Done())

	cacheSyncCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	for rtype, ok := range d.Informers.WaitForCacheSync(cacheSyncCtx.Done()) {
		if !ok {
			logg.Fatal("timeout while waiting for %s cache to sync", rtype.String())
		}
	}
	//silence govet (this is safe because we're not using `cacheSyncCtx` anymore)
	cancel()

	defer d.ProcessingQueue.ShutDown()
	for i := 0; i < threadiness; i++ {
		go d.runWorker(wg)
	}

	<-ctx.Done()
}

func (d *driver) runWorker(wg *sync.WaitGroup) {
	wg.Add(1)
	defer wg.Done()
	logg.Debug("k8s worker: starting...")

	for d.processNextManagedObject() {
	}
	logg.Debug("k8s worker: shutting down...")
}

func (d *driver) processNextManagedObject() (continueWorking bool) {
	queueEntry, shutdown := d.ProcessingQueue.Get()
	if shutdown {
		return false
	}
	defer d.ProcessingQueue.Done(queueEntry)

	ref, ok := queueEntry.(ManagedObjectRef)
	if !ok {
		d.ProcessingQueue.Forget(queueEntry) //to avoid getting this item again
		logg.Error("expected ManagedObjectRef in workqueue, but got %#v", queueEntry)
	}

	success := d.processManagedObject(ref)
	if success {
		d.ProcessingQueue.Forget(queueEntry)
	} else {
		d.ProcessingQueue.AddRateLimited(queueEntry)
	}
	return true
}

func (d *driver) processManagedObject(ref ManagedObjectRef) (ok bool) {
	logg.Debug("k8s worker: processing %s %s", ref.Kind, ref.Name)

	d.ManagedObjectsMutex.RLock()
	mo, exists := d.ManagedObjects[ref]
	d.ManagedObjectsMutex.RUnlock()
	if !exists {
		logg.Debug("ignoring change on unmanaged %s %s", ref.Kind, ref.Name)
		return true
	}

	//converge object towards desired state, if necessary
	var (
		currentState runtime.Object
		newState     runtime.Object
		err          error
	)
	currentState, err = mo.GetCurrentState(d.Informers)
	if err == nil {
		newState, err = mo.CreateOrUpdate(currentState, d.Config)
	}

	//errors go either up to the OrchestrationEngine, or just into the log
	if err != nil {
		if mo.AccountName != "" {
			d.ConnectivityChan <- orchestration.RegistryConnectivityMessage{
				AccountName: mo.AccountName,
				Err:         err,
			}
		} else {
			logg.Error(err.Error())
		}
		return false
	}

	//when touching a Service, notify OrchestrationEngine about its service IP
	if mo.Kind != ObjectKindService {
		return
	}
	svc := newState.(*api_corev1.Service)
	if svc.Spec.ClusterIP != "" {
		d.ConnectivityChan <- orchestration.RegistryConnectivityMessage{
			AccountName: mo.AccountName,
			Host:        svc.Spec.ClusterIP + ":8080",
		}
	}

	return true
}
