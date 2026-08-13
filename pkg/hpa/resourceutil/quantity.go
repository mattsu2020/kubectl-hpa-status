// Package resourceutil provides shared Kubernetes resource quantity helpers.
package resourceutil

import "k8s.io/apimachinery/pkg/api/resource"

// Multiply scales a quantity without converting through MilliValue, which can
// lose precision or overflow for large but valid quantities.
func Multiply(q resource.Quantity, factor int64) resource.Quantity {
	if factor <= 0 || q.IsZero() {
		return resource.Quantity{}
	}
	out := q.DeepCopy()
	out.Mul(factor)
	return out
}
