package dnatcontroller

import (
	"github.com/kubermatic/kubermatic/api/pkg/resources"

	rbacv1 "k8s.io/api/rbac/v1"
)

// ClusterRoleCreator returns the func to create/update the ClusterRole for the DNAT controller
func ClusterRoleCreator() resources.NamedClusterRoleCreatorGetter {
	return func() (string, resources.ClusterRoleCreator) {
		return resources.KubeletDnatControllerClusterRoleName, func(cr *rbacv1.ClusterRole) (*rbacv1.ClusterRole, error) {
			cr.Rules = []rbacv1.PolicyRule{
				{
					APIGroups: []string{""},
					Resources: []string{"nodes"},
					Verbs:     []string{"list", "get", "watch"},
				},
			}
			return cr, nil
		}
	}
}
