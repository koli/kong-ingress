package monitoring

import (
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/golang/glog"
	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/apimachinery/pkg/util/wait"
)

func ListenAndServeHealthz(bindAddress string, port int32) {
	if port > 0 {
		DefaultHealthz()
		go wait.Until(func() {
			addr := net.JoinHostPort(bindAddress, strconv.Itoa(int(port)))
			glog.Infof("Listening healthz service at %s", addr)
			err := http.ListenAndServe(addr, nil)
			if err != nil {
				glog.Errorf("Starting health server failed: %v", err)
			}
		}, 5*time.Second, wait.NeverStop)
	}
}

func ListenAndServeAll(bindAddress string, port int32) {
	if port > 0 {
		DefaultHealthz()
		go wait.Until(func() {
			http.Handle("/metrics", prometheus.Handler())
			addr := net.JoinHostPort(bindAddress, strconv.Itoa(int(port)))
			glog.Infof("Listening monitoring services at %s", addr)
			err := http.ListenAndServe(addr, nil)
			if err != nil {
				glog.Errorf("Starting health and metrics server failed: %v", err)
			}
		}, 5*time.Second, wait.NeverStop)
	}
}
