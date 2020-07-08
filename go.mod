module github.com/open-cluster-management/registration

go 1.13

require (
	github.com/go-bindata/go-bindata v3.1.2+incompatible
	github.com/onsi/ginkgo v1.11.0
	github.com/onsi/gomega v1.8.1
	github.com/open-cluster-management/api v0.0.0-20200610161514-939cead3902c
	github.com/openshift/api v0.0.0-20200521101457-60c476765272
	github.com/openshift/build-machinery-go v0.0.0-20200424080330-082bf86082cc
	github.com/openshift/generic-admission-server v1.14.1-0.20200514123932-ccc9079d8bdb
	github.com/openshift/library-go v0.0.0-20200617154932-eaf8c138def4
	github.com/spf13/cobra v1.0.0
	github.com/spf13/pflag v1.0.5
	k8s.io/api v0.18.3
	k8s.io/apimachinery v0.18.3
	k8s.io/apiserver v0.18.3
	k8s.io/client-go v0.18.3
	k8s.io/component-base v0.18.3
	k8s.io/klog v1.0.0
	k8s.io/kube-aggregator v0.18.3
	k8s.io/utils v0.0.0-20200327001022-6496210b90e8
	sigs.k8s.io/controller-runtime v0.6.0
)
