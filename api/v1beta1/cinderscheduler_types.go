/*
Copyright 2022.

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

package v1beta1

import (
	condition "github.com/openstack-k8s-operators/lib-common/pkg/condition"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CinderSchedulerSpec defines the desired state of CinderScheduler
type CinderSchedulerSpec struct {
	// +kubebuilder:validation:Required
	// ManagingCrName - CR name of managing controller object to identify the config maps
	ManagingCrName string `json:"managingCrName"`

	// +kubebuilder:validation:Optional
	// +kubebuilder:default=cinder
	// ServiceUser - optional username used for this service to register in cinder
	ServiceUser string `json:"serviceUser"`

	// +kubebuilder:validation:Optional
	// ContainerImage - Cinder API Container Image URL
	ContainerImage string `json:"containerImage,omitempty"`

	// +kubebuilder:validation:Required
	// Replicas - Cinder API Replicas
	Replicas int32 `json:"replicas"`

	// +kubebuilder:validation:Required
	// DatabaseHostname - Cinder Database Hostname
	DatabaseHostname string `json:"databaseHostname"`

	// +kubebuilder:validation:Optional
	// +kubebuilder:default=cinder
	// DatabaseUser - optional username used for cinder DB, defaults to cinder
	// TODO: -> implement needs work in mariadb-operator, right now only cinder
	DatabaseUser string `json:"databaseUser,omitempty"`

	// +kubebuilder:validation:Required
	// Secret containing OpenStack password information for CinderDatabasePassword, AdminPassword
	Secret string `json:"secret"`

	// +kubebuilder:validation:Optional
	// PasswordSelectors - Selectors to identify the DB and AdminUser password and TransportURL from the Secret
	PasswordSelectors PasswordSelector `json:"passwordSelectors,omitempty"`

	// +kubebuilder:validation:Optional
	// NodeSelector to target subset of worker nodes for running the Scheduler service
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// +kubebuilder:validation:Optional
	// Debug - enable debug for different deploy stages. If an init container is used, it runs and the
	// actual action pod gets started with sleep infinity
	Debug CinderDebug `json:"debug,omitempty"`
}

// CinderSchedulerStatus defines the observed state of CinderScheduler
type CinderSchedulerStatus struct {
	// Map of hashes to track e.g. job status
	Hash map[string]string `json:"hash,omitempty"`

	// Conditions
	Conditions condition.List `json:"conditions,omitempty" optional:"true"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// CinderScheduler is the Schema for the cinderschedulers API
type CinderScheduler struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CinderSchedulerSpec   `json:"spec,omitempty"`
	Status CinderSchedulerStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// CinderSchedulerList contains a list of CinderScheduler
type CinderSchedulerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CinderScheduler `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CinderScheduler{}, &CinderSchedulerList{})
}
