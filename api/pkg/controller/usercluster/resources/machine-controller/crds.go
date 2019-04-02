package machinecontroller

import (
	"github.com/kubermatic/kubermatic/api/pkg/resources"

	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
)

const (
	clusterAPIGroup   = "cluster.k8s.io"
	clusterAPIVersion = "v1alpha1"
)

// MachineCRD returns the machine CRD definition
func MachineCRDCreator() resources.NamedCustomResourceDefinitionCreatorGetter {
	return func() (string, resources.CustomResourceDefinitionCreator) {
		return resources.MachineCRDName, func(crd *apiextensionsv1beta1.CustomResourceDefinition) (*apiextensionsv1beta1.CustomResourceDefinition, error) {
			crd.Spec.Group = clusterAPIGroup
			crd.Spec.Version = clusterAPIVersion
			crd.Spec.Scope = apiextensionsv1beta1.NamespaceScoped
			crd.Spec.Names.Kind = "Machine"
			crd.Spec.Names.ListKind = "MachineList"
			crd.Spec.Names.Plural = "machines"
			crd.Spec.Names.Singular = "machine"
			crd.Spec.Names.ShortNames = []string{"ma"}

			return crd, nil
		}
	}

}

// MachineSetCRD returns the machineset CRD definition
func MachineSetCRDCreator() resources.NamedCustomResourceDefinitionCreatorGetter {
	return func() (string, resources.CustomResourceDefinitionCreator) {
		return resources.MachineSetCRDName, func(crd *apiextensionsv1beta1.CustomResourceDefinition) (*apiextensionsv1beta1.CustomResourceDefinition, error) {
			crd.Spec.Group = clusterAPIGroup
			crd.Spec.Version = clusterAPIVersion
			crd.Spec.Scope = apiextensionsv1beta1.NamespaceScoped
			crd.Spec.Names.Kind = "MachineSet"
			crd.Spec.Names.ListKind = "MachineSetList"
			crd.Spec.Names.Plural = "machinesets"
			crd.Spec.Names.Singular = "machineset"
			crd.Spec.Names.ShortNames = []string{"ms"}
			crd.Spec.Subresources = &apiextensionsv1beta1.CustomResourceSubresources{Status: &apiextensionsv1beta1.CustomResourceSubresourceStatus{}}

			return crd, nil
		}
	}
}

// MachineDeploymentCRD returns the machinedeployments CRD definition
func MachineDeploymentCRDCreator() resources.NamedCustomResourceDefinitionCreatorGetter {
	return func() (string, resources.CustomResourceDefinitionCreator) {
		return resources.MachineDeploymentCRDName, func(crd *apiextensionsv1beta1.CustomResourceDefinition) (*apiextensionsv1beta1.CustomResourceDefinition, error) {
			crd.Spec.Group = clusterAPIGroup
			crd.Spec.Version = clusterAPIVersion
			crd.Spec.Scope = apiextensionsv1beta1.NamespaceScoped
			crd.Spec.Names.Kind = "MachineDeployment"
			crd.Spec.Names.ListKind = "MachineDeploymentList"
			crd.Spec.Names.Plural = "machinedeployments"
			crd.Spec.Names.Singular = "machinedeployment"
			crd.Spec.Names.ShortNames = []string{"md"}
			crd.Spec.Subresources = &apiextensionsv1beta1.CustomResourceSubresources{Status: &apiextensionsv1beta1.CustomResourceSubresourceStatus{}}

			return crd, nil
		}
	}
}

// ClusterCRD returns the cluster crd definition
func ClusterCRDCreator() resources.NamedCustomResourceDefinitionCreatorGetter {
	return func() (string, resources.CustomResourceDefinitionCreator) {
		return resources.ClusterCRDName, func(crd *apiextensionsv1beta1.CustomResourceDefinition) (*apiextensionsv1beta1.CustomResourceDefinition, error) {
			crd.Spec.Group = clusterAPIGroup
			crd.Spec.Version = clusterAPIVersion
			crd.Spec.Scope = apiextensionsv1beta1.NamespaceScoped
			crd.Spec.Names.Kind = "Cluster"
			crd.Spec.Names.ListKind = "ClusterList"
			crd.Spec.Names.Plural = "clusters"
			crd.Spec.Names.Singular = "cluster"
			crd.Spec.Names.ShortNames = []string{"cl"}
			crd.Spec.Subresources = &apiextensionsv1beta1.CustomResourceSubresources{Status: &apiextensionsv1beta1.CustomResourceSubresourceStatus{}}

			return crd, nil
		}
	}
}
