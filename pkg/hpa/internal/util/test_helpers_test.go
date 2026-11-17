package util

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// mapMeta is a tiny test helper to build an ObjectMeta with name/namespace/labels.
func mapMeta(name, namespace string, labels map[string]string) metav1.ObjectMeta {
	return metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: labels}
}
