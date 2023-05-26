// Package cache implements a ManifestWork Executor Validator with caching capabilities.
// It stores the result of whether the executor has operation permission(SubjectAccessReview)
// on a specific resource in a 2-level cache data structure, the first-level cache is the
// executor key, and the second-level cache is the description (dimension) of the operated
// resource. At the same time, it also contains a controller, which watches the RBAC
// resources(role, roleBinding, clusterRole, clusterRoleBinding) related to the executors
// used by the ManifestWorks in the cluster, and refresh the cache results of the
// corresponding executor when these RBAC resources have any changes.
package cache
