package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"kolihub.io/kong-ingress/pkg/controller"
	"kolihub.io/kong-ingress/pkg/kong"
	"kolihub.io/kong-ingress/pkg/version"

	"github.com/golang/glog"
	"github.com/spf13/pflag"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubescheme "k8s.io/client-go/kubernetes/scheme"
)

// TODO: test with wipeondelete (on/off)
// TODO: delete a primary domain
// TODO: delete a shared domain
// TODO: set a deletionTimestamp on a domain resource
// TODO: test the API recursiviness

const (
	// The default namespace to store the cluster primary domains.
	// If the controller isn't running inside a pod, then this namespace
	// will be used as default to store the domains.
	defaultNamespace        = "kong-system"
	minimalMinorKongVersion = 10
)

// Version refers to the version of the binary
type Version struct {
	git       string
	main      string
	buildDatr string
}

var cfg controller.Config
var showVersion bool

func init() {
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	pflag.StringVar(&cfg.Host, "apiserver", "", "api server addr, e.g. 'http://127.0.0.1:8080'. Omit parameter to run in on-cluster mode and utilize the service account token.")
	pflag.StringVar(&cfg.TLSConfig.CertFile, "cert-file", "", "path to public TLS certificate file.")
	pflag.StringVar(&cfg.TLSConfig.KeyFile, "key-file", "", "path to private TLS certificate file.")
	pflag.StringVar(&cfg.TLSConfig.CAFile, "ca-file", "", "path to TLS CA file.")
	pflag.StringVar(&cfg.KongAdminHost, "kong-server", "", "kong admin api service, e.g. 'http://127.0.0.1:8001'")
	pflag.BoolVar(&cfg.AutoClaim, "auto-claim", false, "try to claim hosts on new ingresses")
	pflag.Int64Var(&cfg.ResyncOnFailed, "resync-on-fail", 60, "time to resync a domain in a failed state phase in seconds")
	pflag.BoolVar(&cfg.WipeOnDelete, "wipe-on-delete", false, "wipe all orphan kong apis when deleting a domain resource")
	pflag.StringVar(&cfg.ClusterDNS, "cluster-dns", "svc.cluster.local", "kubernetes cluster dns name, used to configure the upstream apis in Kong")
	pflag.StringVar(&cfg.PodNamespace, "pod-namespace", defaultNamespace, "the namespace to store cluster primary domains. It will be ignored if is running inside a kubernetes pod")

	pflag.BoolVar(&showVersion, "version", false, "print version information and quit")
	pflag.BoolVar(&cfg.TLSInsecure, "tls-insecure", false, "don't verify API server's CA certificate.")
	pflag.Parse()
	// Convinces goflags that we have called Parse() to avoid noisy logs.
	// OSS Issue: kubernetes/kubernetes#17162.
	flag.CommandLine.Parse([]string{})
}

func main() {
	if showVersion {
		version := version.Get()
		b, err := json.Marshal(&version)
		if err != nil {
			fmt.Printf("failed decoding version: %s\n", err)
			os.Exit(1)
		}
		fmt.Println(string(b))
		return
	}
	var config *rest.Config
	var err error

	if len(cfg.Host) == 0 {
		config, err = rest.InClusterConfig()
		if err != nil {
			glog.Fatalf("error creating client configuration: %v", err)
		}
	} else {
		config = &rest.Config{
			Host:            cfg.Host,
			TLSClientConfig: cfg.TLSConfig,
		}
	}

	var extConfig *rest.Config
	extConfig = config
	extConfig.APIPath = "/apis"
	extConfig.GroupVersion = &kong.SchemeGroupVersion
	extConfig.ContentType = runtime.ContentTypeJSON
	extConfig.NegotiatedSerializer = serializer.DirectCodecFactory{
		CodecFactory: kubescheme.Codecs,
	}
	kong.SchemeBuilder.AddToScheme(kubescheme.Scheme)
	extClient, err := rest.RESTClientFor(extConfig)
	if err != nil {
		glog.Fatalf("failed retrieving extensions client: %v", err)
	}
	kubeClient := kubernetes.NewForConfigOrDie(config)

	kongcli, err := kong.NewKongRESTClient(&rest.Config{Host: cfg.KongAdminHost, Timeout: time.Second * 2})
	if err != nil {
		glog.Fatalf("failed retriveving client config for kong: %s", err)
	}

	kongVersion, err := getKongVersion(kongcli)
	if err != nil {
		glog.Fatalf("failed retrieving kong version: %s", err)
	}
	glog.Infof("Kong Version: %s", kongVersion)
	if kongVersion.Minor < minimalMinorKongVersion {
		glog.Fatalf("unsupported version, require 0.%d.0+", minimalMinorKongVersion)
	}

	if len(os.Getenv("POD_NAMESPACE")) == 0 {
		if err := createDefaultNamespace(kubeClient); err != nil {
			glog.Fatal(err.Error())
		}
	} else {
		cfg.PodNamespace = os.Getenv("POD_NAMESPACE")
	}
	if err := controller.CreateCRD(apiextensionsclient.NewForConfigOrDie(config)); err != nil {
		glog.Fatalf("failed creating domains TPR: %s", err)
	}

	go controller.NewKongController(
		kubeClient,
		extClient,
		kongcli,
		&cfg,
		time.Second*120,
	).Run(1, wait.NeverStop)
	select {} // block forever
}

func createDefaultNamespace(clientset kubernetes.Interface) error {
	ns := &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: defaultNamespace,
		},
	}
	if _, err := clientset.Core().Namespaces().Create(ns); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("Failed creating default namespace [%s]", err)
	}
	return nil
}

func getKongVersion(kongcli *kong.CoreClient) (*kong.KongVersion, error) {
	kv := &kong.KongVersion{}
	var kongInfo map[string]interface{}
	data, err := kongcli.RESTClient().Get().RequestURI("/").DoRaw()
	if err != nil {
		return kv, err
	}
	if err := json.Unmarshal(data, &kongInfo); err != nil {
		return kv, err
	}
	version, ok := kongInfo["version"]
	if !ok {
		return kv, fmt.Errorf("could not extract a version from object: %v", kongInfo)
	}
	v := strings.Split(version.(string), ".")
	if len(v) < 3 {
		return kv, fmt.Errorf("version not in semantic version: %v", v)
	}
	for _, vString := range v {
		if _, err := strconv.Atoi(vString); err != nil {
			return kv, fmt.Errorf("failed converting version: %s", err)
		}
	}
	kv.Major, _ = strconv.Atoi(v[0])
	kv.Minor, _ = strconv.Atoi(v[1])
	kv.Patch, _ = strconv.Atoi(v[2])
	return kv, nil
}
