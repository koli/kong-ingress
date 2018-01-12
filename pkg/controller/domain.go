package controller

import (
	"fmt"
	"time"

	"kolihub.io/kong-ingress/pkg/kong"

	"github.com/golang/glog"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (k *KongController) addDomain(obj interface{}) {
	k.domQueue.Add(obj)
}

func (k *KongController) updateDomain(o, n interface{}) {
	old := o.(*kong.Domain)
	new := n.(*kong.Domain)
	// primaryDomain and Sub must be immutable once is set, this is a workaround
	// to cleanup the records associated with the old resource.
	if old.Spec.PrimaryDomain != new.Spec.PrimaryDomain || old.Spec.Sub != new.Spec.Sub {
		d := &kong.Domain{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%s", old.Name, GenAdler32Hash(new.Spec.PrimaryDomain+new.Spec.Sub)),
				Namespace: old.Namespace,
				// DeletionTimestamp: &metav1.Time{Time: time.Now().UTC()},
			},
			Spec:   old.Spec,
			Status: old.Status,
		}
		d.Status.DeletionTimestamp = &metav1.Time{Time: time.Now().UTC()}
		res, err := k.extClient.Post().
			Resource("domains").
			Namespace(d.Namespace).
			Body(d).
			DoRaw()
		if err != nil {
			glog.Warningf("%s/%s - failed recovering resource %s [%s]", d.Namespace, d.Name, string(res), err)
		}
	}
	if old.ResourceVersion != new.ResourceVersion || new.Status.Phase == kong.DomainStatusFailed {
		k.domQueue.Add(n)
	}
}

func (k *KongController) deleteDomain(obj interface{}) {
	k.domQueue.Add(obj)
}

func (k *KongController) syncDomain(key string, numRequeues int) error {
	obj, exists, err := k.infDom.GetStore().GetByKey(key)
	if err != nil {
		return err
	}

	// https://github.com/kubernetes/kubernetes/issues/40715
	// When the finalizer issue for 3PR is solved it will be possible
	// to garbage collect the resources more effeciently.
	if !exists {
		glog.V(4).Infof("%s - the domain resource doesn't exist.", key)
		if !k.cfg.WipeOnDelete {
			return nil
		}
		// It's not possible to determine if the resource
		// was a primary or a shared domain. First wipe all orphan
		// shared domains in the cluster.
		var parents []*kong.Domain
		if err := cache.ListAll(k.infDom.GetStore(), labels.Everything(), func(o interface{}) {
			d := o.(*kong.Domain)
			if !d.IsPrimary() && d.Status.Phase == kong.DomainStatusOK {
				parents = append(parents, d)
			}
		}); err != nil {
			return fmt.Errorf("gc=true, wipeondelete=true, failed listing domains from store [%s]", err)
		}
		// Find a primary domain for each shared domain, if the primary
		// doesn't exists, wipe all kong apis related to the domain and update
		// the status of the domain in k8s.
		for _, d := range parents {
			primaryDomain, err := SearchForPrimary(k.infDom.GetIndexer(), d, k.cfg.PodNamespace)
			if err != nil {
				return fmt.Errorf("gc=true, wipeondelete=true, failed searching for primary [%s]", err)
			}
			if primaryDomain == nil {
				glog.V(4).Infof("%s - gc=true, wipeondelete=true, primary domain not found for shared %s[%s]", key, d.GetPrimaryDomain(), d.Spec.Sub)
				apiList, err := k.kongcli.API().ListByRegexp(nil, d.GetDomain()+"~.+")
				if err != nil {
					return fmt.Errorf("gc=true, wipeondelete=true, failed listing kong apis [%s]", err)
				}
				for _, api := range apiList.Items {
					glog.V(4).Infof("%s - gc=true, wipeondelete=true, removing kong api %s[%s]", key, api.Name, api.UID)
					if err := k.kongcli.API().Delete(api.Name); err != nil {
						return fmt.Errorf("gc=true, wipeondelete=true, failed removing kong api [%s]", err)
					}
					apisTotal.Dec()
				}
				if err := k.updateDomainStatus(d, "DomainDeleted", "The primary domain was deleted", kong.DomainStatusFailed); err != nil {
					return fmt.Errorf("gc=true, wipeondelete=true, failed updating domain status [%s]", err)
				}
			}
		}

		// Search and wipe all kong apis that doesn't have a domain resource
		// associate with it.
		apiList, err := k.kongcli.API().List(nil)
		if err != nil {
			return fmt.Errorf("gc=true, wipeondelete=true, failed listing kong apis: %s", err)
		}
		for kongHost, apis := range getApisByHost(apiList) {
			var dom *kong.Domain
			if err := cache.ListAll(k.infDom.GetStore(), labels.Everything(), func(o interface{}) {
				if dom != nil {
					return // stop processing, already found the host
				}
				d := o.(*kong.Domain)
				if kongHost == d.GetDomain() {
					dom = d
				}
			}); err != nil {
				return fmt.Errorf("gc=true, wipeondelete=true, failed listing domains from store [%s]", err)
			}
			if dom == nil {
				// The API exists in Kong and there isn't a Domain resource on
				// kubernetes, wipe all related apis
				glog.V(3).Infof("%s - gc=true, wipeondelete=true, missing domain resource for the kong API[%s]", key, kongHost)
				for _, api := range apis {
					glog.V(4).Infof("%s - gc=true, wipeondelete=true, removing kong api %s[%s]", key, api.Name, api.UID)
					if err := k.kongcli.API().Delete(api.Name); err != nil {
						return fmt.Errorf("gc=true, wipeondelete=true, failed removing api [%s]", err)
					}
					apisTotal.Dec()
				}
				continue
			}
			glog.V(4).Infof("%s - gc=true, wipeondelete=true, found a kong API[%s], belonging to a %s domain[%s], skip ...", key,
				kongHost, dom.GetDomainType(), dom.Name)
		}
		return nil
	}
	d := obj.(*kong.Domain)
	if !d.IsValidDomain() {
		glog.V(4).Infof("%s - The domain specified isn't valid", key)
		k.updateDomainStatus(d, "Invalid", "The domain specified on the resource is invalid", kong.DomainStatusFailed)
		return nil
	}
	if d.IsMarkedForDeletion() {
		var domains []*kong.Domain
		if err := cache.ListAll(k.infDom.GetStore(), labels.Everything(), func(o interface{}) {
			dom := o.(*kong.Domain)
			// If the resource its a primary domain, find all associated resources
			if d.IsPrimary() && dom.GetPrimaryDomain() == d.GetPrimaryDomain() {
				domains = append(domains, dom)
			}
		}); err != nil {
			return fmt.Errorf("gc=true, failed listing domains from cache [%s]", err)
		}
		// Purge all associated records
		for _, dom := range domains {
			glog.V(3).Infof("%s - gc=true, found %s[type:%s]!", key, dom.GetDomain(), dom.GetDomainType())
			apiList, err := k.kongcli.API().ListByRegexp(nil, dom.GetDomain()+"~.+")
			if err != nil {
				return fmt.Errorf("gc=true, failed listing kong apis [%s]", err)
			}
			for _, api := range apiList.Items {
				glog.V(4).Infof("%s - gc=true, removing kong api %s[%s]", key, api.Name, api.UID)
				if err := k.kongcli.API().Delete(api.Name); err != nil {
					return fmt.Errorf("gc=true, failed removing kong api [%s]", err)
				}
				apisTotal.Dec()
			}
			if err := k.updateDomainStatus(dom, "DomainDeleted", "The primary domain was deleted", kong.DomainStatusFailed); err != nil {
				return fmt.Errorf("gc=true, failed updating domain status [%s]", err)
			}
		}
		// Delete the domain, all routes have been deleted
		res, err := k.extClient.Patch(types.MergePatchType).
			Resource("domains").
			Name(d.Name).
			Namespace(d.Namespace).
			Body([]byte(`{"metadata": {"finalizers": []}}`)).
			DoRaw()
		if err != nil {
			return fmt.Errorf("gc=true, failed removing finalizer from resource [%s, %s]", string(res), err)
		}
		// TODO: Removing the finalizer doesn't remove the resource automatically
		res, err = k.extClient.Delete().
			Resource("domains").
			Name(d.Name).
			Namespace(d.Namespace).
			DoRaw()
		if err != nil {
			return fmt.Errorf("gc=true, failed removing domain resource [%s, %s]", string(res), err)
		}
		glog.V(4).Infof("%s - gc=true, domain purged %s[primary:%v]", key, d.GetDomain(), d.IsPrimary())
		return nil
	}

	phase := d.Status.Phase
	if phase == "" {
		phase = "NEW"
	}
	glog.V(3).Infof("%s - Status[%v] lastUpdated[%s]", key, phase, d.Status.LastUpdateTime.Format(time.RFC3339))
	switch d.Status.Phase {
	case kong.DomainStatusNew:
		dCopy := d.DeepCopy()
		if dCopy == nil {
			return fmt.Errorf("failed deep copying [%v]", d)
		}
		dCopy.Status.Phase = kong.DomainStatusPending
		dCopy.Finalizers = []string{kong.Finalizer}
		res, err := k.extClient.Put().
			Resource("domains").
			Name(d.Name).
			Namespace(d.Namespace).
			Body(dCopy).
			DoRaw()
		if err != nil {
			return fmt.Errorf("failed updating new domain claim [%s, %s]", string(res), err)
		}
		return nil
	case kong.DomainStatusPending:
		if d.IsPrimary() {
			var primaryDomain *kong.Domain
			// Find if there's a primary domain already registered in the cluster
			cache.ListAll(k.infDom.GetStore(), labels.Everything(), func(o interface{}) {
				// The primary domain was found, stop the search for each object
				if primaryDomain != nil {
					return
				}
				dom := o.(*kong.Domain)
				// skip the search on the target resource
				if d.Name == dom.Name && d.Namespace == dom.Namespace {
					return
				}
				if dom.IsPrimary() && dom.GetPrimaryDomain() == d.GetPrimaryDomain() {
					primaryDomain = dom
				}
			})
			if primaryDomain != nil {
				msg := "The primary domain already exists"
				k.recorder.Event(d, v1.EventTypeWarning, "DomainAlreadyExists", msg)
				glog.Infof("%s - %s, source of conflict [%s/%s]", key, msg, primaryDomain.Namespace, primaryDomain.Name)
				if err := k.updateDomainStatus(d, "DomainAlreadyExists", "The domain already exists", kong.DomainStatusFailed); err != nil {
					return fmt.Errorf("failed updating domain status [%s]", err)
				}
				return nil
			}
			if err := k.updateDomainStatus(d, "", "Primary domain claimed with success", kong.DomainStatusOK); err != nil {
				return fmt.Errorf("failed updating domain status [%s]", err)
			}
			k.recorder.Event(d, v1.EventTypeNormal, "OK", "Primary domain claimed with success")
		} else {
			if !d.IsValidSharedDomain() {
				k.recorder.Event(d, v1.EventTypeWarning, "Invalid", "The shared domain must be a subdomain from the primary")
				return nil
			}
			// Search for a primary domain following the order below:
			// 1) In the parent namespace
			//   The parent namespace must explicity allow permission to be claimed
			// 2) In its own namespace
			//   Any delegation validation isn't performed
			// 3) In the system namespace
			//   Same rule as the first
			// A negative response means the object doesn't have
			// permission to create the shared domain.
			pd, err := SearchForPrimary(k.infDom.GetIndexer(), d, k.cfg.PodNamespace)
			if err != nil {
				return fmt.Errorf("failed retrieving primary domain [%s]", err)
			}
			if pd == nil {
				k.recorder.Event(d, v1.EventTypeWarning, "DomainNotFound", "Primary domain not found")
				if err := k.updateDomainStatus(d, "DomainNotFound", "Primary domain not found", kong.DomainStatusFailed); err != nil {
					return fmt.Errorf("failed updating domain status [%s]", err)
				}
				return nil
			}
			glog.V(3).Infof("%s - Found a primary domain at %s/%s", key, pd.Namespace, pd.Name)
			if err := k.updateDomainStatus(d, "", "Shared domain claimed with success", kong.DomainStatusOK); err != nil {
				return fmt.Errorf("failed updating domain status [%s]", err)
			}
		}
		return nil
	case kong.DomainStatusOK:
		// Ensure that shared domains have their proper parents
		if !d.IsPrimary() {
			glog.V(4).Infof("%s - validating the resource state", key)
			pd, err := SearchForPrimary(k.infDom.GetIndexer(), d, k.cfg.PodNamespace)
			if err != nil {
				return err
			}
			if pd == nil {
				k.recorder.Event(d, v1.EventTypeWarning, "DomainNotFound", "Primary domain not found")
				if err := k.updateDomainStatus(d, "DomainNotFound", "Primary domain not found", kong.DomainStatusFailed); err != nil {
					return fmt.Errorf("failed updating domain status [%s]", err)
				}
			}
		}
	default:
		if d.IsUpdateExpired(time.Second * time.Duration(k.cfg.ResyncOnFailed)) {
			glog.V(3).Infof("%s - update expired, requeueing ...", key)
			statusPhase := kong.DomainStatusPending
			if !d.HasKongFinalizer() {
				statusPhase = kong.DomainStatusNew
			}
			if err := k.updateDomainStatus(d, "", "", statusPhase); err != nil {
				glog.Infof("%s - failed updating domain status [%s]", key, err)
				return nil // let the requeue kicks in to reprocess the key
			}
		}
	}
	return nil
}

func (k *KongController) updateDomainStatus(d *kong.Domain, reason, message string, phase kong.DomainPhase) error {
	// don't update if the status hasn't changed
	if d.Status.Phase == phase && d.Status.Message == message && d.Status.Reason == reason {
		return nil
	}
	dCopy := d.DeepCopy()
	if dCopy == nil {
		return fmt.Errorf("failed deep copying object [%v]", d)
	}
	dCopy.Status.Phase = phase
	dCopy.Status.Reason = reason
	dCopy.Status.Message = message
	dCopy.Status.LastUpdateTime = time.Now().UTC()
	r, err := k.extClient.Put().
		Resource("domains").
		Name(dCopy.Name).
		Namespace(dCopy.Namespace).
		Body(dCopy).
		DoRaw()
	if err != nil {
		return fmt.Errorf("failed updating domain status [%s, %s]", err, string(r))
	}
	return nil
}
