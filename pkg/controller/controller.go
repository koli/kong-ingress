package controller

import (
	"fmt"
	"time"

	// "kolihub.io/kong-ingress/pkg/kong"

	"github.com/golang/glog"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/pkg/api"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/util/workqueue"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/apis/extensions/v1beta1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"

	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	// corelisters "k8s.io/kubernetes/pkg/client/listers/core/v1"
	"kolihub.io/kong-ingress/pkg/kong"
)

var (
	keyFunc = cache.DeletionHandlingMetaNamespaceKeyFunc
)

type KongController struct {
	client  kubernetes.Interface
	kongcli *kong.CoreClient

	infIng cache.SharedIndexInformer
	infSvc cache.SharedIndexInformer

	clusterDNS string

	queue    workqueue.RateLimitingInterface
	recorder record.EventRecorder
}

func NewKongController(client kubernetes.Interface, kongcli *kong.CoreClient, clusterDNS string, resyncPeriod time.Duration) *KongController {
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(glog.Infof)
	eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{
		Interface: v1core.New(client.Core().RESTClient()).Events(""),
	})
	kc := &KongController{
		client:     client,
		kongcli:    kongcli,
		recorder:   eventBroadcaster.NewRecorder(api.Scheme, v1.EventSource{Component: "kong-controller"}),
		clusterDNS: clusterDNS,
		queue:      workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
	}

	// Ingress handlers
	// ingressHandlers :=

	kc.infIng = cache.NewSharedIndexInformer(
		cache.NewListWatchFromClient(client.Extensions().RESTClient(), "ingresses", api.NamespaceAll, fields.Everything()),
		&v1beta1.Ingress{},
		resyncPeriod,
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
	)
	kc.infIng.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			ing := obj.(*v1beta1.Ingress)
			if !isKongIngress(ing) {
				glog.Infof("ignoring add for ingress %v based on annotation %v", ing.Name, ingressClassKey)
				return
			}
			kc.enqueue(obj)
		},
		UpdateFunc: func(o, n interface{}) {
			old := o.(*v1beta1.Ingress)
			new := n.(*v1beta1.Ingress)
			if old.ResourceVersion != new.ResourceVersion && isKongIngress(new) {
				kc.enqueue(n)
			}
		},
		DeleteFunc: func(obj interface{}) {
			ing := obj.(*v1beta1.Ingress)
			if !isKongIngress(ing) {
				glog.Infof("ignoring delete for ingress %v based on annotation %v", ing.Name, ingressClassKey)
				return
			}
			kc.enqueue(obj)
		},
	})

	kc.infSvc = cache.NewSharedIndexInformer(
		cache.NewListWatchFromClient(client.Core().RESTClient(), "services", api.NamespaceAll, fields.Everything()),
		&v1.Service{},
		resyncPeriod,
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
	)

	return kc
}

func (k *KongController) enqueue(obj interface{}) {
	key, err := keyFunc(obj)
	if err != nil {
		glog.Infof("couldn't get key for object %+v: %v", obj, err)
		return
	}
	k.queue.Add(key)
}

// Run starts the kong controller.
func (k *KongController) Run(workers int, stopc <-chan struct{}) {
	glog.Infof("starting Kong controller")
	// don't let panics crash the process
	defer utilruntime.HandleCrash()
	defer k.queue.ShutDown()

	go k.infIng.Run(stopc)
	go k.infSvc.Run(stopc)

	if !cache.WaitForCacheSync(stopc, k.infIng.HasSynced, k.infSvc.HasSynced) {
		return
	}

	// start up your worker threads based on threadiness.
	for i := 0; i < workers; i++ {
		// runWorker will loop until "something bad" happens.
		// The .Until will then rekick the worker after one second
		go wait.Until(k.runWorker, time.Second, stopc)
	}

	// run will loop until "something bad" happens.
	// It will rekick the worker after one second
	// go k.ingQueue.run(time.Second, k.stopCh)
	<-stopc
	glog.Infof("shutting down Kong controller")
}

func (k *KongController) runWorker() {
	for {
		key, quit := k.queue.Get()
		if quit {
			return
		}
		defer k.queue.Done(key)

		glog.V(3).Infof("syncing %v", key)
		err := k.syncHandler(key.(string))
		if err == nil {
			k.queue.Forget(key)
			return
		}
		utilruntime.HandleError(fmt.Errorf("%v failed with : %v", key, err))
		k.queue.AddRateLimited(key)
	}
}

func (k *KongController) syncHandler(key string) error {
	obj, exists, err := k.infIng.GetStore().GetByKey(key)
	if err != nil {
		return err
	}
	if !exists {
		// TODO: Garbage Collect kong apis
		return nil
	}
	ing := obj.(*v1beta1.Ingress)
	// TODO: add tls
	for _, r := range ing.Spec.Rules {
		if r.HTTP == nil {
			// the ingress resource should not accept this kind of scenario
			glog.Errorf("%s - missing rule values for ingress", key)
			k.recorder.Event(ing, v1.EventTypeWarning, "MissingRuleValue", "Missing http ingress rule value")
			continue
		}
		// TODO: validate if it's a valid hostname
		if len(r.Host) == 0 {
			glog.Errorf("%s - path based routing unsupported at the moment", key)
			k.recorder.Event(ing, v1.EventTypeWarning, "Unsupported", "Path based routing is unsupported at the moment")
			continue
		}
		// Path based routing is unsupported with kong
		// Wait for 0.10 release: https://github.com/Mashape/kong/issues/369
		// Only the first path is used for now
		b := r.HTTP.Paths[0].Backend
		serviceExists := false
		// warn if the service doesn't exists
		cache.ListAll(k.infSvc.GetStore(), labels.Everything(), func(obj interface{}) {
			svc := obj.(*v1.Service)
			if svc.Name == b.ServiceName && svc.Namespace == ing.Namespace {
				serviceExists = true
			}
		})
		if !serviceExists {
			k.recorder.Eventf(ing, v1.EventTypeWarning, "ServiceNotFound", "Service %s not found for ingress", b.ServiceName)
		}

		proto := "http"
		if b.ServicePort.IntVal == 443 {
			proto = "https"
		}

		apiBody := &kong.API{
			RequestHost: r.Host,
			UpstreamURL: k.getUpstream(proto, ing.Namespace, b.ServiceName),
			// TODO: annotation feature
			PreserveHost: false,
		}

		k.kongcli.API().Delete(r.Host)
		resp, err := k.kongcli.API().UpdateOrCreate(apiBody)
		if err != nil && !apierrors.IsConflict(err) {
			k.recorder.Eventf(ing, v1.EventTypeWarning, "FailedAddRoute", "%s", err)
			return fmt.Errorf("%s - failed adding api: %s", key, err)
		}
		glog.Infof("%s - added route for: %s[%s]", key, r.Host, resp.UID)
	}

	return nil
}

func (k *KongController) getUpstream(proto, ns, svcName string) string {
	return fmt.Sprintf("%s://%s.%s.%s",
		proto,
		svcName,
		ns,
		k.clusterDNS)
}
