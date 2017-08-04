package controller

import (
	"bytes"
	"fmt"
	"net/url"
	"reflect"
	"time"

	"github.com/golang/glog"
	"kolihub.io/kong-ingress/pkg/kong"

	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/apis/extensions/v1beta1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
)

// TODO: an user is limited on how many paths and hosts he could create, this limitation is based on a hard quota from a Plan
// Wait for https://github.com/Mashape/kong/issues/383

var (
	keyFunc = cache.DeletionHandlingMetaNamespaceKeyFunc
)

// KongController watches the kubernetes api server and adds/removes apis on Kong
type KongController struct {
	client    kubernetes.Interface
	tprClient rest.Interface
	kongcli   *kong.CoreClient

	infIng cache.SharedIndexInformer
	infSvc cache.SharedIndexInformer
	infDom cache.SharedIndexInformer

	cfg *Config

	ingQueue *TaskQueue
	domQueue *TaskQueue
	svcQueue *TaskQueue
	recorder record.EventRecorder
}

// NewKongController creates a new KongController
func NewKongController(client kubernetes.Interface, tprClient rest.Interface, kongcli *kong.CoreClient, cfg *Config, resyncPeriod time.Duration) *KongController {
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(glog.Infof)
	eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{
		Interface: v1core.New(client.Core().RESTClient()).Events(""),
	})
	kc := &KongController{
		client:    client,
		tprClient: tprClient,
		kongcli:   kongcli,
		recorder:  eventBroadcaster.NewRecorder(api.Scheme, v1.EventSource{Component: "kong-controller"}),
		cfg:       cfg,
	}
	kc.ingQueue = NewTaskQueue(kc.syncIngress, "kong_ingress_queue")
	kc.domQueue = NewTaskQueue(kc.syncDomain, "kong_domain_queue")
	kc.svcQueue = NewTaskQueue(kc.syncServices, "kong_service_queue")

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
			kc.ingQueue.Add(obj)
		},
		UpdateFunc: func(o, n interface{}) {
			old := o.(*v1beta1.Ingress)
			new := n.(*v1beta1.Ingress)
			if old.ResourceVersion != new.ResourceVersion && isKongIngress(new) {
				kc.ingQueue.Add(n)
				return
			}
		},
		DeleteFunc: func(obj interface{}) {
			ing := obj.(*v1beta1.Ingress)
			if !isKongIngress(ing) {
				glog.Infof("ignoring delete for ingress %v based on annotation %v", ing.Name, ingressClassKey)
				return
			}
			kc.ingQueue.Add(obj)
		},
	})

	kc.infSvc = cache.NewSharedIndexInformer(
		cache.NewListWatchFromClient(client.Core().RESTClient(), "services", api.NamespaceAll, fields.Everything()),
		&v1.Service{},
		resyncPeriod,
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
	)

	kc.infSvc.AddEventHandler(cache.ResourceEventHandlerFuncs{
		DeleteFunc: func(obj interface{}) {
			kc.svcQueue.Add(obj)
		},
		UpdateFunc: func(o, n interface{}) {
			old := o.(*v1.Service)
			new := n.(*v1.Service)
			if old.ResourceVersion != new.ResourceVersion {
				kc.svcQueue.Add(n)
			}
		},
		AddFunc: func(obj interface{}) {
			kc.svcQueue.Add(obj)
		},
	})

	kc.infDom = cache.NewSharedIndexInformer(
		cache.NewListWatchFromClient(tprClient, "domains", api.NamespaceAll, fields.Everything()),
		&kong.Domain{},
		resyncPeriod,
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
	)
	kc.infDom.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    kc.addDomain,
		UpdateFunc: kc.updateDomain,
		DeleteFunc: kc.deleteDomain,
	})
	return kc
}

// Run starts the kong controller.
func (k *KongController) Run(workers int, stopc <-chan struct{}) {
	glog.Infof("Starting Kong controller")
	// don't let panics crash the process
	defer utilruntime.HandleCrash()
	defer k.ingQueue.shutdown()
	defer k.domQueue.shutdown()
	defer k.svcQueue.shutdown()

	go k.infIng.Run(stopc)
	go k.infSvc.Run(stopc)
	go k.infDom.Run(stopc)

	if !cache.WaitForCacheSync(stopc, k.infIng.HasSynced, k.infSvc.HasSynced) {
		return
	}

	// start up your worker threads based on threadiness.
	for i := 0; i < workers; i++ {
		// runWorker will loop until "something bad" happens.
		// The .Until will then rekick the worker after one second
		go k.ingQueue.run(time.Second, stopc)
		go k.domQueue.run(time.Second, stopc)
		go k.svcQueue.run(time.Second, stopc)
	}

	// run will loop until "something bad" happens.
	// It will rekick the worker after one second
	<-stopc
	glog.Infof("Shutting down Kong controller")
}

// garbage collect kong apis
func (k *KongController) syncServices(key string, numRequeues int) error {
	obj, exists, err := k.infSvc.GetStore().GetByKey(key)
	if err != nil {
		return err
	}
	if !exists {
		glog.V(4).Infof("%s - gc=true, service resource doesn't exists", key)
		return nil
	}
	svc := obj.(*v1.Service)
	if svc.DeletionTimestamp == nil {
		return nil
	}

	for _, port := range svc.Spec.Ports {
		proto := "http"
		if port.Port == 443 {
			proto = "https"
		}
		upstreamURL := k.getUpstream(proto, svc.Namespace, svc.Name, port.Port)
		glog.V(4).Infof("%s - gc=true, cleaning up kong apis from upstream %s", key, upstreamURL)
		params := url.Values{"upstream_url": []string{upstreamURL}}
		apiList, err := k.kongcli.API().List(params)
		if err != nil {
			return fmt.Errorf("gc=true, failed listing apis [%s]", err)
		}
		for _, api := range apiList.Items {
			glog.V(4).Infof("%s - gc=true, removing kong api %s[%s]", key, api.Name, api.UID)
			if err := k.kongcli.API().Delete(api.Name); err != nil {
				return fmt.Errorf("gc=true, failed removing kong api %s, [%s]", api.Name, err)
			}
		}
		// remove the finalizer
		if _, err := k.client.Core().Services(svc.Namespace).Patch(
			svc.Name,
			types.MergePatchType,
			[]byte(`{"metadata": {"finalizers": []}}`),
		); err != nil {
			return fmt.Errorf("gc=true, failed patch service [%s]", err)
		}
	}
	return nil
}

func (k *KongController) syncIngress(key string, numRequeues int) error {
	obj, exists, err := k.infIng.GetStore().GetByKey(key)
	if err != nil {
		glog.V(4).Infof("%s - failed retrieving object from store: %s", key, err)
		return err
	}
	if !exists {
		glog.V(4).Infof("%s - ingress doesn't exists", key)
		return nil
	}

	ing := obj.(*v1beta1.Ingress)
	if numRequeues > autoClaimMaxRetries {
		// The dirty state is used only to indicate the object couldn't recover
		// from a bad state, useful to warn clients.
		if err := k.setDirty(ing, numRequeues); err != nil {
			glog.Warningf("%s - failed set resource as dirty: %s", err)
		}
	}

	if k.cfg.AutoClaim {
		if err := k.claimDomains(ing); err != nil {
			return fmt.Errorf("autoclaim=on, failed claiming domains [%s]", err)
		}
		time.Sleep(500 * time.Millisecond) // give time to sync the domains
	}
	isAllowed, notFoundHost, err := k.isClaimed(ing)
	if err != nil {
		return fmt.Errorf("failed retrieving domains from indexer [%s]", err)
	}
	if !isAllowed {
		if numRequeues > 2 {
			k.recorder.Eventf(ing, v1.EventTypeWarning, "DomainNotFound", "The domain '%s' wasn't claimed, check its state", notFoundHost)
		}
		return fmt.Errorf("failed claiming domain %s, check its state!", notFoundHost)
	}
	glog.V(4).Infof("%s - Allowed to sync ingress routes, found all domains.", key)
	// TODO: add tls
	// Rules could have repeated domains, it will be redundant but it will work.
	for _, r := range ing.Spec.Rules {
		if r.HTTP == nil {
			glog.V(4).Infof("%s - HTTP is nil, skipping ...")
			continue
		}
		// Iterate for each path and generate a new API registry on kong.
		// A domain will have multiple endpoints allowing path based routing.
		for _, p := range r.HTTP.Paths {
			// TODO: validate the service port!
			serviceExists := false
			err := cache.ListAll(k.infSvc.GetStore(), labels.Everything(), func(obj interface{}) {
				svc := obj.(*v1.Service)
				if svc.Name == p.Backend.ServiceName && svc.Namespace == ing.Namespace {
					serviceExists = true
				}
			})
			if err != nil {
				return fmt.Errorf("failed listing services from cache: %s", err)
			}
			if !serviceExists {
				k.recorder.Eventf(ing, v1.EventTypeWarning, "ServiceNotFound", "Service '%s' not found for ingress", p.Backend.ServiceName)
				return fmt.Errorf("Service %s not found", p.Backend.ServiceName)
			}
			// A finalizer is necessary to clean the APIs associated with kong.
			// A service is directly related with several Kong APIs by its upstream.
			fdata := fmt.Sprintf(`{"metadata": {"finalizers": ["%s"]}}`, kong.Finalizer)
			finalizerPatchData := bytes.NewBufferString(fdata).Bytes()
			if _, err := k.client.Core().Services(ing.Namespace).Patch(
				p.Backend.ServiceName,
				types.StrategicMergePatchType,
				finalizerPatchData,
			); err != nil {
				return fmt.Errorf("failed configuring service: %s", err)
			}

			proto := "http"
			if p.Backend.ServicePort.IntVal == 443 {
				proto = "https"
			}
			upstreamURL := k.getUpstream(
				proto,
				ing.Namespace,
				p.Backend.ServiceName,
				p.Backend.ServicePort.IntVal,
			)
			pathURI := p.Path
			// An empty path or root one (/) has no distinction in Kong.
			// Normalize the path otherwise it will generate a distinct adler hash
			if pathURI == "/" || pathURI == "" {
				pathURI = "/"
			}
			apiName := fmt.Sprintf("%s~%s~%s", r.Host, ing.Namespace, GenAdler32Hash(pathURI))
			api, resp := k.kongcli.API().Get(apiName)
			if resp.Error() != nil && !apierrors.IsNotFound(resp.Error()) {
				k.recorder.Eventf(ing, v1.EventTypeWarning, "FailedAddRoute", "%s", resp)
				return fmt.Errorf("failed listing api: %s", resp)
			}

			apiBody := &kong.API{
				Name:        apiName,
				Hosts:       []string{r.Host},
				UpstreamURL: upstreamURL,
			}
			if p.Path != "" {
				apiBody.URIs = []string{pathURI}
			}
			// It will trigger an update when providing the uuid,
			// otherwise a new record will be created.
			if api != nil {
				apiBody.UID = api.UID
				apiBody.CreatedAt = api.CreatedAt
			}
			api, resp = k.kongcli.API().UpdateOrCreate(apiBody)
			if resp.Error() != nil && !apierrors.IsConflict(resp.Error()) {
				return fmt.Errorf("failed adding api: %s", resp)
			}
			glog.Infof("%s - added route for %s[%s]", key, r.Host, api.UID)
		}
	}
	return nil
}

func (k *KongController) getUpstream(proto, ns, svcName string, svcPort int32) string {
	return fmt.Sprintf("%s://%s.%s.%s:%d",
		proto,
		svcName,
		ns,
		k.cfg.ClusterDNS,
		svcPort)
}

func (k *KongController) claimDomains(ing *v1beta1.Ingress) error {
	for _, d := range getHostsFromIngress(ing) {
		domainType := d.GetDomainType()
		if !d.IsValidDomain() {
			return fmt.Errorf("it's not a valid domain %s", d.GetDomain())
		}
		obj, exists, _ := k.infDom.GetStore().Get(d)
		glog.V(4).Infof("%s/%s - Trying to claim %s domain %s ...", ing.Namespace, ing.Name, domainType, d.GetDomain())
		if exists {
			dom := obj.(*kong.Domain)
			if reflect.DeepEqual(dom.Spec, d.Spec) {
				glog.V(4).Infof("%s/%s - Skip update on %s host %s, any changes found ...", ing.Namespace, ing.Name, domainType, d.GetDomain())
				continue
			}
			glog.Infof("%s/%s - Updating %s domain %s ...", ing.Namespace, ing.Name, domainType, d.GetDomain())
			domCopy, err := dom.DeepCopy()
			if err != nil {
				return fmt.Errorf("failed deep copying resource [%s]", err)
			}
			domCopy.Spec = d.Spec
			// If the domain exists, try to recover its status requeuing as a new domain
			if domCopy.Status.Phase != kong.DomainStatusOK {
				domCopy.Status = kong.DomainStatus{Phase: kong.DomainStatusNew}
			}
			res, err := k.tprClient.Put().
				Resource("domains").
				Name(domCopy.Name).
				Namespace(ing.Namespace).
				Body(domCopy).
				DoRaw()
			if err != nil {
				return fmt.Errorf("failed updating domain [%s, %s]", string(res), err)
			}

		} else {
			res, err := k.tprClient.Post().
				Resource("domains").
				Namespace(ing.Namespace).
				Body(d).
				DoRaw()
			if err != nil {
				return fmt.Errorf("failed creating new domain [%s, %s]", string(res), err)
			}
		}
	}
	return nil
}

// isClaimed validates if a domain exists and is allowed to be claimed (DomainStatusOK)
// for each host on an ingress resource.
func (k *KongController) isClaimed(ing *v1beta1.Ingress) (bool, string, error) {
	for _, rule := range ing.Spec.Rules {
		var d *kong.Domain
		err := cache.ListAllByNamespace(k.infDom.GetIndexer(), ing.Namespace, labels.Everything(), func(obj interface{}) {
			if d != nil {
				return // the host is allowed, stop processing further resources
			}
			dom := obj.(*kong.Domain)
			if dom.Status.Phase == kong.DomainStatusOK && dom.GetDomain() == rule.Host {
				d = dom
			}
		})
		if err != nil || d == nil {
			return false, rule.Host, err
		}
		glog.V(4).Infof("%s/%s - Found %s domain %s!", ing.Namespace, ing.Name, d.GetDomainType(), d.GetDomain())
	}
	return true, "", nil
}

// setDirty sets an annotation indicating the object could not recover from itself
func (k *KongController) setDirty(ing *v1beta1.Ingress, retries int) error {
	payload := []byte(`{"metadata": {"annotations": {"kolihub.io/dirty": "true"}}}`)
	if ing.Annotations != nil && ing.Annotations["kolihub.io/dirty"] == "true" {
		return nil // it's already dirty
	}
	glog.Infof("%s/%s - retries[%d], the object could not recover from itself, setting as dirty.", ing.Namespace, ing.Name, retries)
	_, err := k.client.Extensions().Ingresses(ing.Namespace).
		Patch(ing.Name, types.StrategicMergePatchType, payload)
	return err
}
