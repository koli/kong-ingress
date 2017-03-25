package controller

import (
	"crypto/md5"
	"encoding/hex"

	"k8s.io/client-go/pkg/apis/extensions/v1beta1"
)

const (
	// ingressClassKey picks a specific "class" for the Ingress. The controller
	// only processes Ingresses with this annotation either unset, or set
	// to either gceIngessClass or the empty string.
	ingressClassKey  = "kubernetes.io/ingress.class"
	kongIngressClass = "kong"
	// kong ingress maximum size rules
	hardRuleSizeQuota = 5
	// kong ingress maximum size paths
	hardPathSizeQuota = 5
	kongFinalizer     = "kolihub.io/kong"
)

// ingAnnotations represents Ingress annotations.
type ingAnnotations map[string]string

func (ing ingAnnotations) ingressClass() string {
	val, ok := ing[ingressClassKey]
	if !ok {
		return ""
	}
	return val
}

// isKongIngress returns true if the given Ingress either doesn't specify the
// ingress.class annotation, or it's set to "kong".
func isKongIngress(ing *v1beta1.Ingress) bool {
	class := ingAnnotations(ing.ObjectMeta.Annotations).ingressClass()
	return class == "" || class == kongIngressClass
}

// GenMD5Hash generates a md5 from a given string
func GenMD5Hash(text string) string {
	hasher := md5.New()
	hasher.Write([]byte(text))
	return hex.EncodeToString(hasher.Sum(nil))
}
