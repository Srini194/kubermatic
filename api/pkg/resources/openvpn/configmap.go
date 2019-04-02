package openvpn

import (
	"fmt"
	"net"
	"strings"

	kubermaticv1 "github.com/kubermatic/kubermatic/api/pkg/crd/kubermatic/v1"
	"github.com/kubermatic/kubermatic/api/pkg/resources"

	corev1 "k8s.io/api/core/v1"
)

type serverClientConfigsData interface {
	Cluster() *kubermaticv1.Cluster
	NodeAccessNetwork() string
}

// ServerClientConfigsConfigMapCreator returns a ConfigMap containing the ClientConfig for the OpenVPN server. It lives inside the seed-cluster
func ServerClientConfigsConfigMapCreator(data serverClientConfigsData) resources.NamedConfigMapCreatorGetter {
	return func() (string, resources.ConfigMapCreator) {
		return resources.OpenVPNClientConfigsConfigMapName, func(cm *corev1.ConfigMap) (*corev1.ConfigMap, error) {
			cm.Labels = resources.BaseAppLabel(name, nil)

			var iroutes []string

			// iroute for pod network
			if len(data.Cluster().Spec.ClusterNetwork.Pods.CIDRBlocks) < 1 {
				return nil, fmt.Errorf("cluster.Spec.ClusterNetwork.Pods.CIDRBlocks must contain at least one entry")
			}
			_, podNet, err := net.ParseCIDR(data.Cluster().Spec.ClusterNetwork.Pods.CIDRBlocks[0])
			if err != nil {
				return nil, err
			}
			iroutes = append(iroutes, fmt.Sprintf("iroute %s %s",
				podNet.IP.String(),
				net.IP(podNet.Mask).String()))

			// iroute for service network
			if len(data.Cluster().Spec.ClusterNetwork.Services.CIDRBlocks) < 1 {
				return nil, fmt.Errorf("cluster.Spec.ClusterNetwork.Services.CIDRBlocks must contain at least one entry")
			}
			_, serviceNet, err := net.ParseCIDR(data.Cluster().Spec.ClusterNetwork.Services.CIDRBlocks[0])
			if err != nil {
				return nil, err
			}
			iroutes = append(iroutes, fmt.Sprintf("iroute %s %s",
				serviceNet.IP.String(),
				net.IP(serviceNet.Mask).String()))

			_, nodeAccessNetwork, err := net.ParseCIDR(data.NodeAccessNetwork())
			if err != nil {
				return nil, fmt.Errorf("failed to parse node access network %s: %v", data.NodeAccessNetwork(), err)
			}
			iroutes = append(iroutes, fmt.Sprintf("iroute %s %s",
				nodeAccessNetwork.IP.String(),
				net.IP(nodeAccessNetwork.Mask).String()))

			if cm.Data == nil {
				cm.Data = map[string]string{}
			}

			// trailing newline
			iroutes = append(iroutes, "")
			cm.Data["user-cluster-client"] = strings.Join(iroutes, "\n")

			return cm, nil
		}
	}
}
