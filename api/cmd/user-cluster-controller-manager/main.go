package main

import (
	"context"
	"errors"
	"flag"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/golang/glog"
	"github.com/heptiolabs/healthcheck"
	"github.com/oklog/run"

	"github.com/kubermatic/kubermatic/api/pkg/controller/ipam"
	"github.com/kubermatic/kubermatic/api/pkg/controller/nodecsrapprover"
	"github.com/kubermatic/kubermatic/api/pkg/controller/rbac-user-cluster"
	"github.com/kubermatic/kubermatic/api/pkg/controller/usercluster"

	apiextensionv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	certutil "k8s.io/client-go/util/cert"
	apiregistrationv1beta1 "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1beta1"
	clusterv1alpha1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/runtime/signals"
)

type controllerRunOptions struct {
	metricsListenAddr string
	healthListenAddr  string
	openshift         bool
	networks          networkFlags
	namespace         string
	caPath            string
	clusterURL        string
	openvpnServerPort int
}

func main() {
	runOp := controllerRunOptions{}
	flag.StringVar(&runOp.metricsListenAddr, "metrics-listen-address", "127.0.0.1:8085", "The address on which the internal HTTP /metrics server is running on")
	flag.StringVar(&runOp.healthListenAddr, "health-listen-address", "127.0.0.1:8086", "The address on which the internal HTTP /ready & /live server is running on")
	flag.BoolVar(&runOp.openshift, "openshift", false, "Whether the managed cluster is an openshift cluster")
	flag.Var(&runOp.networks, "ipam-controller-network", "The networks from which the ipam controller should allocate IPs for machines (e.g.: .--ipam-controller-network=10.0.0.0/16,10.0.0.1,8.8.8.8 --ipam-controller-network=192.168.5.0/24,192.168.5.1,1.1.1.1,8.8.4.4)")
	flag.StringVar(&runOp.namespace, "namespace", "", "Namespace in which the cluster is running in")
	flag.StringVar(&runOp.caPath, "ca-cert", "ca.crt", "Path to the CA cert file")
	flag.StringVar(&runOp.clusterURL, "cluster-url", "", "Cluster URL")
	flag.IntVar(&runOp.openvpnServerPort, "openvpn-server-port", 0, "OpenVPN server port")
	flag.Parse()

	if runOp.namespace == "" {
		log.Fatal("-namespace must be set")
	}
	if runOp.caPath == "" {
		log.Fatal("-ca-cert must be set")
	}
	if runOp.clusterURL == "" {
		log.Fatal("-cluster-url must be set")
	}
	clusterURL, err := url.Parse(runOp.clusterURL)
	if err != nil {
		log.Fatal(err)
	}
	if runOp.openvpnServerPort == 0 {
		log.Fatal("-openvpn-server-port must be set")
	}

	caBytes, err := ioutil.ReadFile(runOp.caPath)
	if err != nil {
		log.Fatal(err)
	}
	certs, err := certutil.ParseCertsPEM(caBytes)
	if err != nil {
		log.Fatal(err)
	}
	if len(certs) != 1 {
		log.Fatalf("did not find exactly one but %d certificates in the given CA", len(certs))
	}

	var g run.Group

	healthHandler := healthcheck.NewHandler()

	cfg, err := config.GetConfig()
	if err != nil {
		glog.Fatal(err)
	}
	stopCh := signals.SetupSignalHandler()
	ctx, ctxDone := context.WithCancel(context.Background())
	defer ctxDone()

	// Create Context
	done := ctx.Done()

	mgr, err := manager.New(cfg, manager.Options{LeaderElection: true, LeaderElectionNamespace: metav1.NamespaceSystem, MetricsBindAddress: runOp.metricsListenAddr})
	if err != nil {
		glog.Fatal(err)
	}

	glog.Info("registering components")
	if err := apiextensionv1beta1.AddToScheme(mgr.GetScheme()); err != nil {
		glog.Fatal(err)
	}
	if err := apiregistrationv1beta1.AddToScheme(mgr.GetScheme()); err != nil {
		glog.Fatal(err)
	}

	// Setup all Controllers
	glog.Info("registering controllers")
	if err := usercluster.Add(mgr,
		runOp.openshift,
		runOp.namespace,
		certs[0],
		clusterURL,
		runOp.openvpnServerPort,
		healthHandler.AddReadinessCheck); err != nil {
		glog.Fatalf("failed to register user cluster controller: %v", err)
	}

	if len(runOp.networks) > 0 {
		if err := clusterv1alpha1.AddToScheme(mgr.GetScheme()); err != nil {
			glog.Fatalf("failed to add clusterv1alpha1 scheme: %v", err)
		}
		if err := ipam.Add(mgr, runOp.networks); err != nil {
			glog.Fatalf("failed to add IPAM controller to mgr: %v", err)
		}
		glog.Infof("Added IPAM controller to mgr")
	}

	if err := rbacusercluster.Add(mgr, healthHandler.AddReadinessCheck); err != nil {
		glog.Fatalf("failed to add user RBAC controller to mgr: %v", err)
	}

	if runOp.openshift {
		if err := nodecsrapprover.Add(mgr, 4, cfg); err != nil {
			glog.Fatalf("failed to add nodecsrapprover controller: %v", err)
		}
		glog.Infof("Registered nodecsrapprover controller")
	}

	// This group is forever waiting in a goroutine for signals to stop
	{
		g.Add(func() error {
			select {
			case <-stopCh:
				return errors.New("user requested to stop the application")
			case <-done:
				return errors.New("parent context has been closed - propagating the request")
			}
		}, func(err error) {
			ctxDone()
		})
	}

	// This group starts the controller manager
	{
		g.Add(func() error {
			// Start the Cmd
			return mgr.Start(done)
		}, func(err error) {
			glog.Infof("stopping user cluster controller manager, err = %v", err)
		})
	}

	// This group starts the readiness & liveness http server
	{
		h := &http.Server{Addr: runOp.healthListenAddr, Handler: healthHandler}
		g.Add(func() error {
			return h.ListenAndServe()
		}, func(err error) {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
			defer cancel()

			if err := h.Shutdown(shutdownCtx); err != nil {
				glog.Errorf("Healthcheck handler terminated with an error: %v", err)
			}
		})
	}

	if err := g.Run(); err != nil {
		glog.Fatal(err)
	}

}
