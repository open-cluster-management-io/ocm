package v1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ConsoleExternalLogLink is an extension for customizing OpenShift web console log links.
//
// Compatibility level 2: Stable within a major release for a minimum of 9 months or 3 minor releases (whichever is longer).
// +kubebuilder:object:root=true
// +kubebuilder:resource:path=consoleexternalloglinks,scope=Cluster
// +kubebuilder:subresource:status
// +openshift:api-approved.openshift.io=https://github.com/openshift/api/pull/481
// +openshift:file-pattern=operatorOrdering=00
// +openshift:capability=Console
// +kubebuilder:metadata:annotations="description=ConsoleExternalLogLink is an extension for customizing OpenShift web console log links."
// +kubebuilder:metadata:annotations="displayName=ConsoleExternalLogLinks"
// +kubebuilder:printcolumn:name=Text,JSONPath=.spec.text,type=string
// +kubebuilder:printcolumn:name=HrefTemplate,JSONPath=.spec.hrefTemplate,type=string
// +kubebuilder:printcolumn:name=Age,JSONPath=.metadata.creationTimestamp,type=date
// +openshift:compatibility-gen:level=2
type ConsoleExternalLogLink struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is the standard object's metadata.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#metadata
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec ConsoleExternalLogLinkSpec `json:"spec"`
}

// ConsoleExternalLogLinkSpec is the desired log link configuration.
// The log link will appear on the logs tab of the pod details page.
type ConsoleExternalLogLinkSpec struct {
	// text is the display text for the link
	Text string `json:"text"`
	// hrefTemplate is an absolute secure URL (must use https) for the log link including
	// variables to be replaced. Variables are specified in the URL with the format ${variableName},
	// for instance, ${containerName} and will be replaced with the corresponding values
	// from the resource. Resource is a pod.
	// Supported variables are:
	// - ${resourceName} - name of the resource which containes the logs
	// - ${resourceUID} - UID of the resource which contains the logs
	//               - e.g. `11111111-2222-3333-4444-555555555555`
	// - ${containerName} - name of the resource's container that contains the logs
	// - ${resourceNamespace} - namespace of the resource that contains the logs
	// - ${resourceNamespaceUID} - namespace UID of the resource that contains the logs
	// - ${podLabels} - JSON representation of labels matching the pod with the logs
	//             - e.g. `{"key1":"value1","key2":"value2"}`
	//
	// e.g., https://example.com/logs?resourceName=${resourceName}&containerName=${containerName}&resourceNamespace=${resourceNamespace}&podLabels=${podLabels}
	// +kubebuilder:validation:Pattern=`^https://`
	HrefTemplate string `json:"hrefTemplate"`
	// namespaceFilter is a regular expression used to restrict a log link to a
	// matching set of namespaces (e.g., `^openshift-`). The string is converted
	// into a regular expression using the JavaScript RegExp constructor.
	// If not specified, links will be displayed for all the namespaces.
	// +optional
	NamespaceFilter string `json:"namespaceFilter,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Compatibility level 2: Stable within a major release for a minimum of 9 months or 3 minor releases (whichever is longer).
// +openshift:compatibility-gen:level=2
type ConsoleExternalLogLinkList struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is the standard list's metadata.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#metadata
	metav1.ListMeta `json:"metadata"`

	Items []ConsoleExternalLogLink `json:"items"`
}
