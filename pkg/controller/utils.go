package controller

import (
	"fmt"
	"hash/adler32"
	"strconv"
	"strings"
	"time"

	"kolihub.io/kong-ingress/pkg/kong"

	"github.com/golang/glog"
	"github.com/juju/ratelimit"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	extensions "k8s.io/api/extensions/v1beta1"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// ingressClassKey picks a specific "class" for the Ingress. The controller
	// only processes Ingresses with this annotation either unset, or set
	// to either gceIngessClass or the empty string.
	ingressClassKey  = "kubernetes.io/ingress.class"
	kongIngressClass = "kong"
	// kong ingress maximum size rules
	hardRuleSizeQuota = 5
	// kong ingress maximum size paths
	hardPathSizeQuota   = 5
	autoClaimMaxRetries = 8
)

// ingAnnotations represents Ingress annotations.
type ingAnnotations map[string]string

func (ing ingAnnotations) ingressClass() string {
	val, ok := ing[ingressClassKey]
	if !ok {
		return ""
	}
	return val
}

// isKongIngress returns true if the given Ingress either doesn't specify the
// ingress.class annotation, or it's set to "kong".
func isKongIngress(ing *extensions.Ingress) bool {
	class := ingAnnotations(ing.ObjectMeta.Annotations).ingressClass()
	return class == "" || class == kongIngressClass
}

// TaskQueue manages a work queue through an independent worker that
// invokes the given sync function for every work item inserted.
type TaskQueue struct {
	// queue is the work queue the worker polls
	queue workqueue.RateLimitingInterface
	// sync is called for each item in the queue
	sync func(string, int) error
	// workerDone is closed when the worker exits
	workerDone chan struct{}
}

func (t *TaskQueue) run(period time.Duration, stopCh <-chan struct{}) {
	wait.Until(t.worker, period, stopCh)
}

// Add enqueues ns/name of the given api object in the task queue.
func (t *TaskQueue) Add(obj interface{}) {
	key, err := keyFunc(obj)
	if err != nil {
		glog.Infof("Couldn't get key for object %+v: %v", obj, err)
		return
	}
	t.queue.Add(key)
}

// worker processes work in the queue through sync.
func (t *TaskQueue) worker() {
	for {
		key, quit := t.queue.Get()
		if quit {
			close(t.workerDone)
			return
		}
		if key == nil {
			continue
		}
		numRequeues := t.queue.NumRequeues(key)
		glog.V(3).Infof("Syncing %v", key)
		if err := t.sync(key.(string), numRequeues); err != nil {
			t.queue.AddRateLimited(key)
			glog.Errorf("Requeuing[%d] %v, err: %v", numRequeues, key, err)
		} else {
			t.queue.Forget(key)
		}
		t.queue.Done(key)
	}
}

// shutdown shuts down the work queue and waits for the worker to ACK
func (t *TaskQueue) shutdown() {
	t.queue.ShutDown()
	<-t.workerDone
}

// NewTaskQueue creates a new task queue with the given sync function.
// The sync function is called for every element inserted into the queue.
func NewTaskQueue(syncFn func(string, int) error, queueName string) *TaskQueue {
	rateLimiter := workqueue.NewMaxOfRateLimiter(
		workqueue.NewItemExponentialFailureRateLimiter(300*time.Millisecond, 1000*time.Second),
		// 10 qps, 100 bucket size.  This is only for retry speed and its only the overall factor (not per item)
		&workqueue.BucketRateLimiter{Bucket: ratelimit.NewBucketWithRate(float64(10), int64(100))},
	)
	return &TaskQueue{
		queue:      workqueue.NewNamedRateLimitingQueue(rateLimiter, queueName),
		sync:       syncFn,
		workerDone: make(chan struct{}),
	}
}

// GenAdler32Hash generates a adler32 hash from a given string
func GenAdler32Hash(text string) string {
	adler32Int := adler32.Checksum([]byte(text))
	return strconv.FormatUint(uint64(adler32Int), 16)
}

// CreateCRD provision the Custom Resource Definition and
// wait until the API is ready to interact it with
func CreateCRD(clientset apiextensionsclient.Interface) error {
	crd := &apiextensionsv1beta1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: kong.ResourceName,
		},
		Spec: apiextensionsv1beta1.CustomResourceDefinitionSpec{
			Group:   kong.SchemeGroupVersion.Group,
			Version: kong.SchemeGroupVersion.Version,
			Scope:   apiextensionsv1beta1.NamespaceScoped,
			Names: apiextensionsv1beta1.CustomResourceDefinitionNames{
				Plural: kong.ResourcePlural,
				Kind:   kong.ResourceKind,
				// ShortNames: []string{"domain"},
			},
		},
	}
	_, err := clientset.ApiextensionsV1beta1().CustomResourceDefinitions().Create(crd)
	if err == nil || apierrors.IsAlreadyExists(err) {
		glog.Infof("Custom Resource Definiton '%s' provisioned, waiting to be ready ...", kong.ResourceName)
		return waitCRDReady(clientset)
	}
	return err
}

func waitCRDReady(clientset apiextensionsclient.Interface) error {
	return wait.Poll(1*time.Second, 30*time.Second, func() (bool, error) {
		crd, err := clientset.
			ApiextensionsV1beta1().
			CustomResourceDefinitions().
			Get(kong.ResourceName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		for _, cond := range crd.Status.Conditions {
			switch cond.Type {
			case apiextensionsv1beta1.Established:
				if cond.Status == apiextensionsv1beta1.ConditionTrue {
					return true, nil
				}
			case apiextensionsv1beta1.NamesAccepted:
				if cond.Status == apiextensionsv1beta1.ConditionFalse {
					return false, fmt.Errorf("Name conflict: %v", cond.Reason)
				}
			}
		}
		return false, nil
	})
}

// CreateDomainTPRs generates the third party resource required for interacting with releases
func CreateDomainTPRs(host string, clientset kubernetes.Interface) error {
	tprs := []*extensions.ThirdPartyResource{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "domain.platform.koli.io",
			},
			Versions: []extensions.APIVersion{
				{Name: "v1"},
			},
			Description: "Holds information about domain claims to prevent duplicated hosts in ingress resources",
		},
	}
	tprClient := clientset.Extensions().ThirdPartyResources()
	for _, tpr := range tprs {
		if _, err := tprClient.Create(tpr); err != nil && !apierrors.IsAlreadyExists(err) {
			return err
		}
		glog.Infof("Third Party Resource '%s' provisioned", tpr.Name)
	}

	// We have to wait for the TPRs to be ready. Otherwise the initial watch may fail.
	return wait.Poll(1*time.Second, 30*time.Second, func() (bool, error) {
		_, err := clientset.ExtensionsV1beta1().ThirdPartyResources().Get("domain.platform.koli.io", metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		return true, nil
	})
}

// SearchForPrimary searchs for a primary domain
func SearchForPrimary(indexer cache.Indexer, d *kong.Domain, systemNamespace string) (primaryDomain *kong.Domain, err error) {
	// If the resource is indicating a parent, then search by it first
	if len(d.Spec.Parent) != 0 {
		primaryDomain, err = searchPrimaryByNamespace(indexer, d, d.Spec.Parent)
		if err != nil || primaryDomain != nil {
			return
		}
	}

	// A parent wasn't specified or it isn't allowed, search in the resource namespace
	primaryDomain, err = searchPrimaryByNamespace(indexer, d, d.Namespace)
	if err != nil || primaryDomain != nil {
		return
	}

	// Skip because it was already searched
	if d.Namespace == systemNamespace || d.Spec.Parent == systemNamespace {
		return
	}

	// The last tentative will be searching on the system namespace
	return searchPrimaryByNamespace(indexer, d, systemNamespace)
}

func searchPrimaryByNamespace(indexer cache.Indexer, d *kong.Domain, listByNS string) (primaryDomain *kong.Domain, err error) {
	err = cache.ListAllByNamespace(indexer, listByNS, labels.Everything(), func(obj interface{}) {
		dom := obj.(*kong.Domain)
		// skip, the primaryDomain was already found
		if primaryDomain != nil && !dom.IsPrimary() {
			return
		}
		isNamespaceOwner := false
		if d.Namespace == dom.Namespace {
			isNamespaceOwner = true
		}
		if dom.Status.Phase == kong.DomainStatusOK && dom.GetPrimaryDomain() == d.GetPrimaryDomain() {
			// If the domain resources belongs to the target namespace,
			// the namespace is allowed to claim any domain on it.
			if isNamespaceOwner {
				primaryDomain = dom
				return // stop processing
			}
			// When the target namespace is distinct from the domain resource namespace,
			// a delegation validation is required.
			for _, delegateNS := range dom.Spec.Delegates {
				// delegation could be set to specific domains or to all
				// domains on the cluster (wildcard "*")
				if delegateNS == d.Namespace || delegateNS == "*" {
					primaryDomain = dom
					break
				}
			}
		}
	})
	if err != nil {
		return primaryDomain, fmt.Errorf("failed listing domains from cache: %s", err)
	}
	return
}

// getHostsFromIngress extract the all hosts from a ingress resource
// prepending primary domains
func getHostsFromIngress(ing *extensions.Ingress) (hosts []*kong.Domain) {
	for _, rule := range ing.Spec.Rules {
		primaryDomain, sub := extractPrimaryFromHost(rule.Host)
		var parentNamespace string
		if ing.Annotations != nil {
			parentNamespace = ing.Annotations["kolihub.io/parent"]
			if ing.Annotations[fmt.Sprintf("kolihub.io/%s", rule.Host)] == "primary" {
				primaryDomain, sub = rule.Host, ""
				parentNamespace = "" // a primary doesn't need a parent
			}
		}
		d := &kong.Domain{
			ObjectMeta: metav1.ObjectMeta{
				Name:      genMetaNameFromHost(rule.Host),
				Namespace: ing.Namespace,
			},
			Spec: kong.DomainSpec{
				PrimaryDomain: primaryDomain,
				Sub:           sub,
				Parent:        parentNamespace,
			},
		}
		if d.IsPrimary() {
			hosts = append([]*kong.Domain{d}, hosts...)
			continue
		}
		hosts = append(hosts, d)
	}
	return
}

func extractPrimaryFromHost(host string) (string, string) {
	parts := strings.Split(host, ".")
	// It's a primary domain, a subdomain must have at least 3 segments
	if len(parts) < 3 {
		return host, ""
	}
	return strings.Join(parts[1:], "."), parts[0]
}

// generates a compliance metadata name for the given host string
func genMetaNameFromHost(ingressHost string) string {
	return strings.Join(strings.Split(ingressHost, "."), "-")
}

// returns a list of apis for every host found
func getApisByHost(apiList *kong.APIList) map[string][]kong.API {
	hosts := map[string][]kong.API{}
	for _, api := range apiList.Items {
		// [host]~[namespace]~[path-prefix-hash]
		apiParts := strings.Split(api.Name, "~")
		// skip non-prefixed apis
		if len(apiParts) != 3 {
			continue
		}
		hosts[apiParts[0]] = append(hosts[apiParts[0]], api)
	}
	return hosts
}
