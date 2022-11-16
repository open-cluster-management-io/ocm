/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package options

import (
	"github.com/spf13/pflag"

	"k8s.io/apiserver/pkg/admission"
	"k8s.io/apiserver/pkg/server"
	genericoptions "k8s.io/apiserver/pkg/server/options"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/rest"
	"k8s.io/component-base/featuregate"
)

// AdmissionOptions holds the admission options.
// It is a wrap of generic AdmissionOptions.
type AdmissionOptions struct {
	// GenericAdmission holds the generic admission options.
	GenericAdmission *genericoptions.AdmissionOptions
}

// NewAdmissionOptions creates a new instance of AdmissionOptions
// Note:
//
//	In addition it calls RegisterAllAdmissionPlugins to register
//	all kube-apiserver admission plugins.
//
//	Provides the list of RecommendedPluginOrder that holds sane values
//	that can be used by servers that don't care about admission chain.
//	Servers that do care can overwrite/append that field after creation.
func NewAdmissionOptions() *AdmissionOptions {
	options := genericoptions.NewAdmissionOptions()
	// register all admission plugins
	RegisterAllAdmissionPlugins(options.Plugins)
	// set RecommendedPluginOrder
	options.RecommendedPluginOrder = AllOrderedPlugins
	// set DefaultOffPlugins
	options.DefaultOffPlugins = DefaultOffAdmissionPlugins()

	return &AdmissionOptions{
		GenericAdmission: options,
	}
}

// AddFlags adds flags related to admission for kube-apiserver to the specified FlagSet
func (a *AdmissionOptions) AddFlags(fs *pflag.FlagSet) {
	a.GenericAdmission.AddFlags(fs)
}

// Validate verifies flags passed to kube-apiserver AdmissionOptions.
// Kube-apiserver verifies PluginNames and then call generic AdmissionOptions.Validate.
func (a *AdmissionOptions) Validate() []error {
	return a.GenericAdmission.Validate()
}

// ApplyTo adds the admission chain to the server configuration.
// Kube-apiserver just call generic AdmissionOptions.ApplyTo.
func (a *AdmissionOptions) ApplyTo(
	c *server.Config,
	informers informers.SharedInformerFactory,
	kubeAPIServerClientConfig *rest.Config,
	features featuregate.FeatureGate,
	pluginInitializers ...admission.PluginInitializer,
) error {
	if a == nil {
		return nil
	}

	return a.GenericAdmission.ApplyTo(c, informers, kubeAPIServerClientConfig, features, pluginInitializers...)
}
