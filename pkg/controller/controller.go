package controller

import (
	"context"
	"net/http"
	"time"

	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	cloudprovider "k8s.io/cloud-provider"
	servicecontroller "k8s.io/cloud-provider/controllers/service"
	controllersmetrics "k8s.io/component-base/metrics/prometheus/controllers"
	"k8s.io/klog/v2"
	"sigs.k8s.io/cloud-provider-kind/pkg/constants"
	"sigs.k8s.io/cloud-provider-kind/pkg/container"
	"sigs.k8s.io/cloud-provider-kind/pkg/provider"
	"sigs.k8s.io/kind/pkg/cluster"
	"sigs.k8s.io/kind/pkg/log"
)

type Controller struct {
	kind     *cluster.Provider
	clusters map[string]*ccm
}

type ccm struct {
	factory           informers.SharedInformerFactory
	serviceController *servicecontroller.Controller
	cancelFn          context.CancelFunc
}

func New(logger log.Logger) *Controller {
	controllersmetrics.Register()
	return &Controller{
		kind: cluster.NewProvider(
			cluster.ProviderWithLogger(logger),
		),
		clusters: make(map[string]*ccm),
	}
}

func (c *Controller) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			// cleanup
			containers, err := container.ListByLabel(constants.NodeCCMLabelKey)
			if err != nil {
				klog.Errorf("can't list containers: %w", err)
				return
			}
			for _, id := range containers {
				container.Delete(id)
			}
			return
		default:
		}
		// get existing kind clusters
		clusters, err := c.kind.List()
		if err != nil {
			klog.Infof("error listing clusters, retrying ...: %v", err)
		}

		// add new ones
		for _, cluster := range clusters {
			klog.V(3).Infof("processing cluster %s", cluster)
			_, ok := c.clusters[cluster]
			if ok {
				klog.V(3).Infof("cluster %s already exist", cluster)
				continue
			}

			// get kubeconfig
			kconfig, err := c.kind.KubeConfig(cluster, false)
			if err != nil {
				klog.Errorf("Failed to get kubeconfig for cluster %s: %v", cluster, err)
				continue
			}

			config, err := clientcmd.RESTConfigFromKubeConfig([]byte(kconfig))
			if err != nil {
				klog.Errorf("Failed to convert kubeconfig for cluster %s: %v", cluster, err)
				continue
			}

			kubeClient, err := kubernetes.NewForConfig(config)
			if err != nil {
				klog.Errorf("Failed to create kubeClient for cluster %s: %v", cluster, err)
				continue
			}

			klog.V(2).Infof("Creating new cloud provider for cluster %s", cluster)
			cloud := provider.New(cluster, c.kind)
			ccm, err := startServiceController(ctx, cluster, kubeClient, cloud)
			if err != nil {
				klog.Errorf("Failed to start service controller for cluster %s: %v", cluster, err)
				continue
			}
			klog.Infof("Starting service controller for cluster %s", cluster)
			c.clusters[cluster] = ccm
		}
		// remove expired ones
		clusterSet := sets.New(clusters...)
		for cluster, ccm := range c.clusters {
			_, ok := clusterSet[cluster]
			if !ok {
				klog.Infof("Stopping service controller for cluster %s", cluster)
				ccm.cancelFn()
				delete(c.clusters, cluster)
			}
		}
		time.Sleep(30 * time.Second)
	}
}

func startServiceController(ctx context.Context, clusterName string, kubeClient kubernetes.Interface, cloud cloudprovider.Interface) (*ccm, error) {
	client := kubeClient.Discovery().RESTClient()
	// wait for health
	err := wait.PollImmediateWithContext(ctx, 1*time.Second, 30*time.Second, func(ctx context.Context) (bool, error) {
		healthStatus := 0
		client.Get().AbsPath("/healthz").Do(ctx).StatusCode(&healthStatus)
		if healthStatus != http.StatusOK {
			return false, nil
		}

		return true, nil
	})
	if err != nil {
		klog.Errorf("Failed waiting for apiserver to be ready: %v", err)
		return nil, err
	}

	sharedInformers := informers.NewSharedInformerFactory(kubeClient, 60*time.Second)
	// Start the service controller
	serviceController, err := servicecontroller.New(
		cloud,
		kubeClient,
		sharedInformers.Core().V1().Services(),
		sharedInformers.Core().V1().Nodes(),
		clusterName,
		utilfeature.DefaultFeatureGate,
	)
	if err != nil {
		// This error shouldn't fail. It lives like this as a legacy.
		klog.Errorf("Failed to start service controller: %v", err)
		return nil, err
	}

	ctx, cancel := context.WithCancel(ctx)
	sharedInformers.Start(ctx.Done())
	go serviceController.Run(ctx, 5, controllersmetrics.NewControllerManagerMetrics(clusterName))
	return &ccm{
		factory:           sharedInformers,
		serviceController: serviceController,
		cancelFn:          cancel}, nil
}