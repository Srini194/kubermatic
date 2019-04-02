package cluster

import (
	"fmt"
	"time"

	kubermaticv1 "github.com/kubermatic/kubermatic/api/pkg/crd/kubermatic/v1"
	"github.com/kubermatic/kubermatic/api/pkg/resources"
	"github.com/kubermatic/kubermatic/api/pkg/resources/apiserver"
	"github.com/kubermatic/kubermatic/api/pkg/resources/certificates"
	"github.com/kubermatic/kubermatic/api/pkg/resources/cloudconfig"
	"github.com/kubermatic/kubermatic/api/pkg/resources/controllermanager"
	"github.com/kubermatic/kubermatic/api/pkg/resources/dns"
	"github.com/kubermatic/kubermatic/api/pkg/resources/etcd"
	"github.com/kubermatic/kubermatic/api/pkg/resources/machinecontroller"
	"github.com/kubermatic/kubermatic/api/pkg/resources/metrics-server"
	"github.com/kubermatic/kubermatic/api/pkg/resources/openvpn"
	"github.com/kubermatic/kubermatic/api/pkg/resources/scheduler"
	"github.com/kubermatic/kubermatic/api/pkg/resources/usercluster"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	autoscalingv1beta2 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1beta2"
)

const (
	NodeDeletionFinalizer = "kubermatic.io/delete-nodes"
)

func (cc *Controller) ensureResourcesAreDeployed(cluster *kubermaticv1.Cluster) error {
	data, err := cc.getClusterTemplateData(cluster)
	if err != nil {
		return err
	}

	// check that all services are available
	if cluster.Annotations["kubermatic.io/openshift"] == "" {
		if err := cc.ensureServices(cluster, data); err != nil {
			return err
		}
	}

	// check that all secrets are available // New way of handling secrets
	if cluster.Annotations["kubermatic.io/openshift"] == "" {
		if err := cc.ensureSecrets(cluster, data); err != nil {
			return err
		}
	}

	// check that all StatefulSets are created
	if err := cc.ensureStatefulSets(cluster, data); err != nil {
		return err
	}

	// Wait until the cloud provider infra is ready before attempting
	// to render the cloud-config
	// TODO: Model resource deployment as a DAG so we don't need hacks
	// like this combined with tribal knowledge and "someone is noticing this
	// isn't working correctly"
	// https://github.com/kubermatic/kubermatic/issues/2948
	if !cluster.Status.Health.CloudProviderInfrastructure {
		cc.enqueueAfter(cluster, 1*time.Second)
		return nil
	}

	if cluster.Annotations["kubermatic.io/openshift"] == "" {
		// check that all ConfigMaps are available
		if err := cc.ensureConfigMaps(cluster, data); err != nil {
			return err
		}
	}

	// check that all Deployments are available
	if err := cc.ensureDeployments(cluster, data); err != nil {
		return err
	}

	// check that all CronJobs are created
	if err := cc.ensureCronJobs(cluster, data); err != nil {
		return err
	}

	// check that all PodDisruptionBudgets are created
	if err := cc.ensurePodDisruptionBudgets(cluster, data); err != nil {
		return err
	}

	// check that all StatefulSets are created
	if err := cc.ensureVerticalPodAutoscalers(cluster, data); err != nil {
		return err
	}

	return nil
}

func (cc *Controller) getClusterTemplateData(c *kubermaticv1.Cluster) (*resources.TemplateData, error) {
	dc, found := cc.dcs[c.Spec.Cloud.DatacenterName]
	if !found {
		return nil, fmt.Errorf("failed to get datacenter %s", c.Spec.Cloud.DatacenterName)
	}

	return resources.NewTemplateData(
		c,
		&dc,
		cc.dc,
		cc.secretLister,
		cc.configMapLister,
		cc.serviceLister,
		cc.overwriteRegistry,
		cc.nodePortRange,
		cc.nodeAccessNetwork,
		cc.etcdDiskSize,
		cc.monitoringScrapeAnnotationPrefix,
		cc.inClusterPrometheusRulesFile,
		cc.inClusterPrometheusDisableDefaultRules,
		cc.inClusterPrometheusDisableDefaultScrapingConfigs,
		cc.inClusterPrometheusScrapingConfigsFile,
		cc.dockerPullConfigJSON,
		cc.oidcCAFile,
		cc.oidcIssuerURL,
		cc.oidcIssuerClientID,
		cc.enableEtcdDataCorruptionChecks,
	), nil
}

// ensureNamespaceExists will create the cluster namespace
func (cc *Controller) ensureNamespaceExists(c *kubermaticv1.Cluster) (*kubermaticv1.Cluster, error) {
	var err error
	if c.Status.NamespaceName == "" {
		c, err = cc.updateCluster(c.Name, func(c *kubermaticv1.Cluster) {
			c.Status.NamespaceName = fmt.Sprintf("cluster-%s", c.Name)
		})
		if err != nil {
			return nil, err
		}
	}

	if _, err := cc.namespaceLister.Get(c.Status.NamespaceName); !errors.IsNotFound(err) {
		return c, err
	}

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:            c.Status.NamespaceName,
			OwnerReferences: []metav1.OwnerReference{cc.getOwnerRefForCluster(c)},
		},
	}
	if _, err := cc.kubeClient.CoreV1().Namespaces().Create(ns); err != nil {
		return nil, fmt.Errorf("failed to create namespace %s: %v", c.Status.NamespaceName, err)
	}

	return c, nil
}

// GetServiceCreators returns all service creators that are currently in use
func GetServiceCreators(data *resources.TemplateData) []resources.NamedServiceCreatorGetter {
	return []resources.NamedServiceCreatorGetter{
		apiserver.InternalServiceCreator(),
		apiserver.ExternalServiceCreator(),
		openvpn.ServiceCreator(),
		etcd.ServiceCreator(data),
		dns.ServiceCreator(),
		machinecontroller.ServiceCreator(),
		metricsserver.ServiceCreator(),
	}
}

func (cc *Controller) ensureServices(c *kubermaticv1.Cluster, data *resources.TemplateData) error {
	creators := GetServiceCreators(data)
	return resources.ReconcileServices(creators, c.Status.NamespaceName, cc.dynamicClient, cc.dynamicCache, resources.OwnerRefWrapper(resources.GetClusterRef(c)))
}

// GetDeploymentCreators returns all DeploymentCreators that are currently in use
func GetDeploymentCreators(data *resources.TemplateData) []resources.NamedDeploymentCreatorGetter {
	creators := []resources.NamedDeploymentCreatorGetter{
		openvpn.DeploymentCreator(data),
		dns.DeploymentCreator(data),
	}

	if cluster := data.Cluster(); cluster != nil && cluster.Annotations["kubermatic.io/openshift"] == "" {
		creators = append(creators, apiserver.DeploymentCreator(data))
		creators = append(creators, scheduler.DeploymentCreator(data))
		creators = append(creators, controllermanager.DeploymentCreator(data))
		creators = append(creators, machinecontroller.DeploymentCreator(data))
		creators = append(creators, machinecontroller.WebhookDeploymentCreator(data))
		creators = append(creators, metricsserver.DeploymentCreator(data))
		creators = append(creators, usercluster.DeploymentCreator(data, false))
	}

	return creators
}

func (cc *Controller) ensureDeployments(cluster *kubermaticv1.Cluster, data *resources.TemplateData) error {
	creators := GetDeploymentCreators(data)
	return resources.ReconcileDeployments(creators, cluster.Status.NamespaceName, cc.dynamicClient, cc.dynamicCache, resources.OwnerRefWrapper(resources.GetClusterRef(cluster)))
}

// GetSecretCreators returns all SecretCreators that are currently in use
func (cc *Controller) GetSecretCreators(data *resources.TemplateData) []resources.NamedSecretCreatorGetter {
	creators := []resources.NamedSecretCreatorGetter{
		certificates.RootCACreator(data),
		openvpn.CACreator(),
		certificates.FrontProxyCACreator(),
		resources.ImagePullSecretCreator(cc.dockerPullConfigJSON),
		apiserver.FrontProxyClientCertificateCreator(data),
		etcd.TLSCertificateCreator(data),
		apiserver.EtcdClientCertificateCreator(data),
		apiserver.TLSServingCertificateCreator(data),
		apiserver.KubeletClientCertificateCreator(data),
		apiserver.ServiceAccountKeyCreator(),
		openvpn.TLSServingCertificateCreator(data),
		openvpn.InternalClientCertificateCreator(data),
		machinecontroller.TLSServingCertificateCreator(data),

		// Kubeconfigs
		resources.GetInternalKubeconfigCreator(resources.SchedulerKubeconfigSecretName, resources.SchedulerCertUsername, nil, data),
		resources.GetInternalKubeconfigCreator(resources.KubeletDnatControllerKubeconfigSecretName, resources.KubeletDnatControllerCertUsername, nil, data),
		resources.GetInternalKubeconfigCreator(resources.MachineControllerKubeconfigSecretName, resources.MachineControllerCertUsername, nil, data),
		resources.GetInternalKubeconfigCreator(resources.ControllerManagerKubeconfigSecretName, resources.ControllerManagerCertUsername, nil, data),
		resources.GetInternalKubeconfigCreator(resources.KubeStateMetricsKubeconfigSecretName, resources.KubeStateMetricsCertUsername, nil, data),
		resources.GetInternalKubeconfigCreator(resources.MetricsServerKubeconfigSecretName, resources.MetricsServerCertUsername, nil, data),
		resources.GetInternalKubeconfigCreator(resources.InternalUserClusterAdminKubeconfigSecretName, resources.InternalUserClusterAdminKubeconfigCertUsername, []string{"system:masters"}, data),
		resources.AdminKubeconfigCreator(data),
		apiserver.TokenUsersCreator(data),
	}

	if len(data.OIDCCAFile()) > 0 {
		creators = append(creators, apiserver.DexCACertificateCreator(data))
	}

	return creators
}

func (cc *Controller) ensureSecrets(c *kubermaticv1.Cluster, data *resources.TemplateData) error {
	namedSecretCreatorGetters := cc.GetSecretCreators(data)

	if err := resources.ReconcileSecrets(namedSecretCreatorGetters, c.Status.NamespaceName, cc.dynamicClient, cc.dynamicCache, resources.OwnerRefWrapper(resources.GetClusterRef(c))); err != nil {
		return fmt.Errorf("failed to ensure that the Secret exists: %v", err)
	}

	return nil
}

// GetConfigMapCreators returns all ConfigMapCreators that are currently in use
func GetConfigMapCreators(data *resources.TemplateData) []resources.NamedConfigMapCreatorGetter {
	return []resources.NamedConfigMapCreatorGetter{
		cloudconfig.ConfigMapCreator(data),
		openvpn.ServerClientConfigsConfigMapCreator(data),
		dns.ConfigMapCreator(data),
	}
}

func (cc *Controller) ensureConfigMaps(c *kubermaticv1.Cluster, data *resources.TemplateData) error {
	creators := GetConfigMapCreators(data)

	if err := resources.ReconcileConfigMaps(creators, c.Status.NamespaceName, cc.dynamicClient, cc.dynamicCache, resources.OwnerRefWrapper(resources.GetClusterRef(c))); err != nil {
		return fmt.Errorf("failed to ensure that the ConfigMap exists: %v", err)
	}

	return nil
}

// GetStatefulSetCreators returns all StatefulSetCreators that are currently in use
func GetStatefulSetCreators(data *resources.TemplateData) []resources.NamedStatefulSetCreatorGetter {
	return []resources.NamedStatefulSetCreatorGetter{
		etcd.StatefulSetCreator(data),
	}
}

// GetPodDisruptionBudgetCreators returns all PodDisruptionBudgetCreators that are currently in use
func GetPodDisruptionBudgetCreators(data *resources.TemplateData) []resources.NamedPodDisruptionBudgetCreatorGetter {
	return []resources.NamedPodDisruptionBudgetCreatorGetter{
		etcd.PodDisruptionBudgetCreator(data),
		apiserver.PodDisruptionBudgetCreator(),
		metricsserver.PodDisruptionBudgetCreator(),
	}
}

func (cc *Controller) ensurePodDisruptionBudgets(c *kubermaticv1.Cluster, data *resources.TemplateData) error {
	creators := GetPodDisruptionBudgetCreators(data)

	if err := resources.ReconcilePodDisruptionBudgets(creators, c.Status.NamespaceName, cc.dynamicClient, cc.dynamicCache, resources.OwnerRefWrapper(resources.GetClusterRef(c))); err != nil {
		return fmt.Errorf("failed to ensure that the PodDisruptionBudget exists: %v", err)
	}

	return nil
}

// GetCronJobCreators returns all CronJobCreators that are currently in use
func GetCronJobCreators(data *resources.TemplateData) []resources.NamedCronJobCreatorGetter {
	return []resources.NamedCronJobCreatorGetter{
		etcd.CronJobCreator(data),
	}
}

func (cc *Controller) ensureCronJobs(c *kubermaticv1.Cluster, data *resources.TemplateData) error {
	creators := GetCronJobCreators(data)

	if err := resources.ReconcileCronJobs(creators, c.Status.NamespaceName, cc.dynamicClient, cc.dynamicCache, resources.OwnerRefWrapper(resources.GetClusterRef(c))); err != nil {
		return fmt.Errorf("failed to ensure that the CronJobs exists: %v", err)
	}

	return nil
}

func (cc *Controller) ensureVerticalPodAutoscalers(c *kubermaticv1.Cluster, data *resources.TemplateData) error {
	controlPlaneDeploymentNames := []string{
		"dns-resolver",
		"machine-controller",
		"machine-controller-webhook",
		"openvpn-server",
	}
	if c.Annotations["kubermatic.io/openshift"] == "" {
		controlPlaneDeploymentNames = append(controlPlaneDeploymentNames, "apiserver", "controller-manager", "scheduler", "metrics-server")
	}
	creators, err := resources.GetVerticalPodAutoscalersForAll(controlPlaneDeploymentNames, []string{"etcd"}, c.Status.NamespaceName, cc.dynamicCache)
	if err != nil {
		return fmt.Errorf("failed to create the functions to handle VPA resources: %v", err)
	}

	if !cc.enableVPA {
		// If the feature is disabled, we just wrap the create function to disable the VPA.
		// This is easier than passing a bool to all required functions.
		for i, getNameAndCreator := range creators {
			creators[i] = func() (string, resources.VerticalPodAutoscalerCreator) {
				name, create := getNameAndCreator()
				return name, disableVPAWrapper(create)
			}
		}
	}

	return resources.ReconcileVerticalPodAutoscalers(creators, c.Status.NamespaceName, cc.dynamicClient, cc.dynamicCache, resources.OwnerRefWrapper(resources.GetClusterRef(c)))
}

// disableVPAWrapper is a wrapper function which sets the UpdateMode on the VPA to UpdateModeOff.
// This essentially disables any processing from the VerticalPodAutoscaler
func disableVPAWrapper(create resources.VerticalPodAutoscalerCreator) resources.VerticalPodAutoscalerCreator {
	return func(vpa *autoscalingv1beta2.VerticalPodAutoscaler) (*autoscalingv1beta2.VerticalPodAutoscaler, error) {
		vpa, err := create(vpa)
		if err != nil {
			return nil, err
		}

		if vpa.Spec.UpdatePolicy == nil {
			vpa.Spec.UpdatePolicy = &autoscalingv1beta2.PodUpdatePolicy{}
		}
		mode := autoscalingv1beta2.UpdateModeOff
		vpa.Spec.UpdatePolicy.UpdateMode = &mode

		return vpa, nil
	}
}

func (cc *Controller) ensureStatefulSets(c *kubermaticv1.Cluster, data *resources.TemplateData) error {
	creators := GetStatefulSetCreators(data)

	return resources.ReconcileStatefulSets(creators, c.Status.NamespaceName, cc.dynamicClient, cc.dynamicCache, resources.OwnerRefWrapper(resources.GetClusterRef(c)))
}
