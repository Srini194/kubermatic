package metricsserver

import (
	"github.com/kubermatic/kubermatic/api/pkg/resources"

	rbacv1 "k8s.io/api/rbac/v1"
)

// ClusterRole returns a cluster role for the metrics server
func ClusterRoleCreator() resources.NamedClusterRoleCreatorGetter {
	return func() (string, resources.ClusterRoleCreator) {
		return resources.MetricsServerClusterRoleName, func(cr *rbacv1.ClusterRole) (*rbacv1.ClusterRole, error) {
			cr.Labels = resources.BaseAppLabel(Name, nil)

			cr.Rules = []rbacv1.PolicyRule{
				{
					APIGroups: []string{""},
					Resources: []string{
						"pods",
						"nodes",
						"nodes/stats",
						"namespaces",
					},
					Verbs: []string{
						"get",
						"list",
						"watch",
					},
				},
				{
					APIGroups: []string{"extensions"},
					Resources: []string{
						"deployments",
					},
					Verbs: []string{"get", "list", "watch"},
				},
			}
			return cr, nil
		}
	}
}
