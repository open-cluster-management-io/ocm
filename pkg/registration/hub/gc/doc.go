// package gc contains the hub-side reconciler to cleanup finalizer on
// role/rolebinding in cluster namespace when ManagedCluster is being deleted.
// and delete the cluterRoles for the registration and work agent after therer is no
// cluster and manifestwork.
package gc
