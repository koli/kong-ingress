package controller

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
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
	"bytes"

	"k8s.io/apimachinery/pkg/types"
	"kolihub.io/kong-ingress/pkg/kong"
)

// TODO: an user is limited on how many paths and hosts he could create, this limitation is based on a hard quota from a Plan
// Wait for https://github.com/Mashape/kong/issues/383

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
				return
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
	// Rules could have repeated domains, it will be redundant but it will work.
	for _, r := range ing.Spec.Rules {
		// if r.HTTP == nil {
		// 	// the ingress resource should not accept this kind of scenario
		// 	glog.Errorf("%s - missing rule values for ingress", key)
		// 	k.recorder.Event(ing, v1.EventTypeWarning, "MissingRuleValue", "Missing http ingress rule value")
		// 	continue
		// }
		if !isAllowedHost(r.Host, ing.Namespace) {
			// glog.Infof("%s - Field 'host' in wrong format", key)
			k.recorder.Event(ing, v1.EventTypeWarning, "Unsupported", "Field 'host' in wrong format, expecting: [name]-[namespace].[domain.tld]")
			return fmt.Errorf("%s - field 'host' in wrong format, expect: [name]-[namespace].[domain.tld]", key)
		}

		if r.HTTP == nil {
			// Configure a root route for this domain
			// Skip for now
			continue
		}

		// Iterate for each path and generate a new API registry on kong.
		// A domain will have multiple endpoints allowing path based routing.
		for _, p := range r.HTTP.Paths {
			// TODO: validate the service port!
			serviceExists := false
			cache.ListAll(k.infSvc.GetStore(), labels.Everything(), func(obj interface{}) {
				svc := obj.(*v1.Service)
				if svc.Name == p.Backend.ServiceName && svc.Namespace == ing.Namespace {
					serviceExists = true
				}
			})
			if !serviceExists {
				glog.Infof("%s - Service %s not found", key, p.Backend.ServiceName)
				k.recorder.Eventf(ing, v1.EventTypeWarning, "ServiceNotFound", "Service %s not found for ingress", p.Backend.ServiceName)
				return fmt.Errorf("%s - service '%s' not found", key, p.Backend.ServiceName)
			}

			// This is required?
			proto := "http"
			if p.Backend.ServicePort.IntVal == 443 {
				proto = "https"
			}
			upstreamURL := k.getUpstream(proto, ing.Namespace, p.Backend.ServiceName)
			pathUri := p.Path
			// An empty path or root one (/) has no distinction in Kong.
			// Normalize the path otherwise it will generate a distinct md5
			if p.Path == "/" || p.Path == "" {
				pathUri = p.Path
			}
			apiName := GenMD5Hash(filepath.Join(pathUri, ing.Namespace, r.Host))
			api, resp := k.kongcli.API().Get(apiName)
			if resp.Error() != nil && !apierrors.IsNotFound(resp.Error()) {
				k.recorder.Eventf(ing, v1.EventTypeWarning, "FailedAddRoute", "%s", resp)
				return fmt.Errorf("%s - failed listing api: %s", key, resp)
			}

			// A finalizer is necessary to clean the APIs associated with kong.
			// A service is directly related with several Kong APIs by its upstream.
			fdata := fmt.Sprintf(`{"metadata": {"finalizers": ["%s"]}}`, kongFinalizer)
			finalizerPatchData := bytes.NewBufferString(fdata).Bytes()
			if _, err := k.client.Core().Services(ing.Namespace).Patch(
				p.Backend.ServiceName,
				types.StrategicMergePatchType,
				finalizerPatchData,
			); err != nil {
				glog.Infof("%s - failed configuring service: %v\n", key, resp)
				k.recorder.Eventf(ing, v1.EventTypeWarning, "FailedAddRoute", "%s", err)
				return fmt.Errorf("%s - failed configuring service: %s", key, err)
			}

			apiBody := &kong.API{
				Name:        apiName,
				Hosts:       []string{r.Host},
				UpstreamURL: upstreamURL,
			}
			if p.Path != "" {
				apiBody.URIs = []string{pathUri}
			}
			// It will trigger an update when providing the uuid,
			// otherwise a new record will be created.
			if api != nil {
				apiBody.UID = api.UID
				apiBody.CreatedAt = api.CreatedAt
			}
			api, resp = k.kongcli.API().UpdateOrCreate(apiBody)
			if resp.Error() != nil && !apierrors.IsConflict(resp.Error()) {
				glog.Infof("%s - %v\n", key, resp)
				k.recorder.Eventf(ing, v1.EventTypeWarning, "FailedAddRoute", "%s", resp)
				return fmt.Errorf("%s - failed adding api: %s", key, resp)
			}
			glog.Infof("%s - added route for: %s[%s]", key, r.Host, api.UID)
		}
	}
	return nil
}

func (k *KongController) getUpstream(proto, ns, svcName string) string {
	// TODO: set a port to upstream
	return fmt.Sprintf("%s://%s.%s.%s",
		proto,
		svcName,
		ns,
		k.clusterDNS)
}

// Verifies if it's an allowed host, expect the following format: [app]-[namespace].[domain.tld]
func isAllowedHost(host, namespace string) bool {
	parts := strings.Split(host, ".")
	// it's not a prefixed domain [prefix].[domain].[tld]
	if len(parts) < 3 {
		return false
	}
	prefixParts := strings.Split(parts[0], "-")
	// It's not a organization namespace [namespace]-[customer]-[organization]
	if len(prefixParts) < 3 {
		return false
	}
	// Get the last three records from a prefix, join and compare with
	// the given namespace
	if strings.Join(prefixParts[len(prefixParts)-3:], "-") != namespace {
		return false
	}
	return true
}

// isValidIngress validate if the ingress is in a valid format for integration with Kong
func (h *KongController) isValidIngress(ingress *v1beta1.Ingress) error {
	if len(ingress.Spec.Rules) > hardRuleSizeQuota {
		return fmt.Errorf("Hard rules reached: %d/%d.", len(ingress.Spec.Rules), hardRuleSizeQuota)
	}
	for _, r := range ingress.Spec.Rules {
		if !isAllowedHost(r.Host, ingress.Namespace) {
			return errors.New("Field 'host' in wrong format, expecting: [name].[namespace].[domain.tld].")
		}
		// Every host must be unique on kong, the field is used as an identifier name for an API object.
		// The API allows multiple domains per upstream, creating duplicated hosts will lead to inconsistent
		// kong API rules.
		for _, innerRule := range ingress.Spec.Rules {
			if r.Host == innerRule.Host {
				return errors.New("Found duplicated hosts, they must be unique in rules.")
			}
		}

		if r.HTTP == nil {
			// The rule doesn't have any paths, move to the next one.
			continue
		}
		if len(r.HTTP.Paths) > hardPathSizeQuota {
			return fmt.Errorf("Hard paths reached: %d/%d.", hardPathSizeQuota, len(r.HTTP.Paths))
		}

		// A backend couldn't have distinct backends, kong doesn't allow path based routing yet.
		// This strictly forbides a rule having multiple backends
		var firstBackend string
		for i, p := range r.HTTP.Paths {
			if i == 0 {
				firstBackend = p.Backend.ServiceName
				continue
			}
			if firstBackend != p.Backend.ServiceName {
				return errors.New("Found distinct services (upstreams) per rule.")
			}
		}
	}
	return nil
}
