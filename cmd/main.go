package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"kolihub.io/kong-ingress/pkg/kong"

	"github.com/golang/glog"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	// "k8s.io/kubernetes/pkg/client/clientset_generated/clientset"

	"kolihub.io/kong-ingress/pkg/controller"
	"kolihub.io/kong-ingress/pkg/version"
)

// Version refers to the version of the binary
type Version struct {
	git       string
	main      string
	buildDatr string
}

// Config defines configuration parameters for the Operator.
type Config struct {
	KongAdminHost string
	Host          string
	TLSInsecure   bool
	TLSConfig     rest.TLSClientConfig
	ClusterDNS    string
}

var cfg Config
var showVersion bool

func init() {
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	pflag.StringVar(&cfg.Host, "apiserver", "", "api server addr, e.g. 'http://127.0.0.1:8080'. Omit parameter to run in on-cluster mode and utilize the service account token.")
	pflag.StringVar(&cfg.TLSConfig.CertFile, "cert-file", "", "path to public TLS certificate file.")
	pflag.StringVar(&cfg.TLSConfig.KeyFile, "key-file", "", "path to private TLS certificate file.")
	pflag.StringVar(&cfg.TLSConfig.CAFile, "ca-file", "", "path to TLS CA file.")
	pflag.StringVar(&cfg.KongAdminHost, "kong-server", "", "kong admin api service, e.g. 'http://127.0.0.1:8001'")
	pflag.StringVar(&cfg.ClusterDNS, "cluster-dns", "svc.cluster.local", "kubernetes cluster dns name")

	pflag.BoolVar(&showVersion, "version", false, "print version information and quit")
	pflag.BoolVar(&cfg.TLSInsecure, "tls-insecure", false, "don't verify API server's CA certificate.")
	pflag.Parse()
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
		config = &rest.Config{Host: cfg.Host}
	}
	// config.QPS = 10
	kubeClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		glog.Fatalf("failed retrieving client from config: %v", err)
	}
	kongcli, err := kong.NewKongRESTClient(&rest.Config{Host: cfg.KongAdminHost})
	if err != nil {
		glog.Fatalf("failed retriveving client config for kong: %s", err)
	}
	go controller.NewKongController(
		kubeClient,
		kongcli,
		cfg.ClusterDNS,
		time.Second*30,
	).Run(1, wait.NeverStop)
	select {} // block forever
}
