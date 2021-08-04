module open-cluster-management.io/placement

go 1.16

replace github.com/googleapis/gnostic => github.com/googleapis/gnostic v0.4.1 // ensure compatible between controller-runtime and kube-openapi

require (
	github.com/onsi/ginkgo v1.14.1
	github.com/onsi/gomega v1.10.2
	github.com/openshift/build-machinery-go v0.0.0-20210209125900-0da259a2c359
	github.com/openshift/library-go v0.0.0-20210420183610-0e395da73318
	github.com/spf13/cobra v1.1.3
	github.com/spf13/pflag v1.0.5
	k8s.io/api v0.21.1
	k8s.io/apimachinery v0.21.1
	k8s.io/apiserver v0.21.0-rc.0
	k8s.io/client-go v0.21.1
	k8s.io/component-base v0.21.0
	k8s.io/klog/v2 v2.8.0
	open-cluster-management.io/api v0.0.0-20210610125115-f57c747b84aa
	sigs.k8s.io/controller-runtime v0.8.3
)
