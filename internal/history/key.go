package history

import "fmt"

// SnapshotKey identifies one HPA history stream. Cluster and UID are part of
// the identity on purpose:
//
//   - Cluster keeps two environments that happen to use the same
//     namespace/name pair (dev vs prod "web") from merging their histories
//     when the operator switches kubeconfig contexts.
//   - UID starts a fresh generation whenever the HPA object is deleted and
//     recreated, so a trend analysis never spans two different objects.
type SnapshotKey struct {
	Cluster   string
	Namespace string
	Name      string
	UID       string
}

// String renders the key for diagnostics and map keys.
func (k SnapshotKey) String() string {
	return fmt.Sprintf("%s/%s/%s@%s", k.Cluster, k.Namespace, k.Name, k.UID)
}

// validate reports whether the key carries the mandatory identifiers.
func (k SnapshotKey) validate() error {
	if k.Namespace == "" || k.Name == "" {
		return fmt.Errorf("namespace and name must not be empty")
	}
	return nil
}
