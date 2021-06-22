module open-cluster-management.io/registration

go 1.16

replace github.com/googleapis/gnostic => github.com/googleapis/gnostic v0.4.1 // ensure compatible between controller-runtime and kube-openapi

require (
	github.com/go-bindata/go-bindata v3.1.2+incompatible
	github.com/onsi/ginkgo v1.14.0
	github.com/onsi/gomega v1.10.1
	github.com/openshift/api v0.0.0-20210331193751-3acddb19d360
	github.com/openshift/build-machinery-go v0.0.0-20210209125900-0da259a2c359
	github.com/openshift/generic-admission-server v1.14.1-0.20200903115324-4ddcdd976480
	github.com/openshift/library-go v0.0.0-20210407140145-f831e911c638
	github.com/spf13/cobra v1.1.1
	github.com/spf13/pflag v1.0.5
	k8s.io/api v0.21.1
	k8s.io/apimachinery v0.21.1
	k8s.io/apiserver v0.21.0-rc.0
	k8s.io/client-go v0.21.1
	k8s.io/component-base v0.21.0-rc.0
	k8s.io/klog/v2 v2.8.0
	k8s.io/kube-aggregator v0.21.0-rc.0
	k8s.io/utils v0.0.0-20201110183641-67b214c5f920
	open-cluster-management.io/api v0.0.0-20210610125115-f57c747b84aa
	sigs.k8s.io/controller-runtime v0.6.1-0.20200829232221-efc74d056b24
)
