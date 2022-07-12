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

const (
	// DbSyncHash hash
	DbSyncHash = "dbsync"

	// DeploymentHash hash used to detect changes
	DeploymentHash = "deployment"
)

// CinderSpec defines the desired state of Cinder
type CinderSpec struct {
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=cinder
	// ServiceUser - optional username used for this service to register in cinder
	ServiceUser string `json:"serviceUser"`

	// +kubebuilder:validation:Required
	// MariaDB instance name
	// Right now required by the maridb-operator to get the credentials from the instance to create the DB
	// Might not be required in future
	DatabaseInstance string `json:"databaseInstance,omitempty"`

	// +kubebuilder:validation:Optional
	// +kubebuilder:default=cinder
	// DatabaseUser - optional username used for cinder DB, defaults to cinder
	// TODO: -> implement needs work in mariadb-operator, right now only cinder
	DatabaseUser string `json:"databaseUser"`

	// +kubebuilder:validation:Required
	// Secret containing OpenStack password information for CinderDatabasePassword, AdminPassword
	Secret string `json:"secret,omitempty"`

	// +kubebuilder:validation:Optional
	// PasswordSelectors - Selectors to identify the DB and AdminUser password and TransportURL from the Secret
	PasswordSelectors PasswordSelector `json:"passwordSelectors,omitempty"`

	// +kubebuilder:validation:Optional
	// CinderAPINodeSelector to target subset of worker nodes for running the API service
	CinderAPINodeSelector map[string]string `json:"cinderApiNodeSelector,omitempty"`

	// +kubebuilder:validation:Optional
	// CinderSchedulerNodeSelector to target subset of worker nodes for running the Scheduler service
	CinderSchedulerNodeSelector map[string]string `json:"cinderSchedulerNodeSelector,omitempty"`

	// +kubebuilder:validation:Optional
	// CinderBackupNodeSelector to target subset of worker nodes for running the Backup service
	CinderBackupNodeSelector map[string]string `json:"cinderBackupNodeSelector,omitempty"`

	// +kubebuilder:validation:Optional
	// Debug - enable debug for different deploy stages. If an init container is used, it runs and the
	// actual action pod gets started with sleep infinity
	Debug CinderDebug `json:"debug,omitempty"`

	// +kubebuilder:validation:Optional
	// +kubebuilder:default=false
	// PreserveJobs - do not delete jobs after they finished e.g. to check logs
	PreserveJobs bool `json:"preserveJobs,omitempty"`

	// +kubebuilder:validation:Optional
	// +kubebuilder:default="# add your customization here"
	// CustomServiceConfig - customize the service config using this parameter to change service defaults,
	// or overwrite rendered information using raw OpenStack config format. The content gets added to
	// to /etc/<service>/<service>.conf.d directory as custom.conf file.
	CustomServiceConfig string `json:"customServiceConfig,omitempty"`

	// +kubebuilder:validation:Optional
	// ConfigOverwrite - interface to overwrite default config files like e.g. logging.conf or policy.json.
	// But can also be used to add additional files. Those get added to the service config dir in /etc/<service> .
	// TODO: -> implement
	DefaultConfigOverwrite map[string]string `json:"defaultConfigOverwrite,omitempty"`

	// +kubebuilder:validation:Optional
	// CinderAPIContainerImage - Cinder API Container Image URL
	CinderAPIContainerImage string `json:"cinderAPIContainerImage,omitempty"`

	// +kubebuilder:validation:Optional
	// CinderBackupContainerImage - Cinder Backup Container Image URL
	CinderBackupContainerImage string `json:"cinderBackupContainerImage,omitempty"`

	// +kubebuilder:validation:Optional
	// CinderSchedulerContainerImage - Cinder Scheduler Container Image URL
	CinderSchedulerContainerImage string `json:"cinderSchedulerContainerImage,omitempty"`

	// +kubebuilder:validation:Required
	// CinderAPIReplicas - Cinder API Replicas
	CinderAPIReplicas int32 `json:"cinderAPIReplicas"`

	// +kubebuilder:validation:Required
	// CinderBackupReplicas - Cinder Backup Replicas
	CinderBackupReplicas int32 `json:"cinderBackupReplicas"`

	// +kubebuilder:validation:Required
	// CinderSchedulerReplicas - Cinder Scheduler Replicas
	CinderSchedulerReplicas int32 `json:"cinderSchedulerReplicas"`

	// +kubebuilder:validation:Optional
	// CinderVolumes - cells to create
	CinderVolumes []Volume `json:"cinderVolumes,omitempty"`
}

// Volume defines cinder volume configuration parameters
type Volume struct {
	// Name of cinder volume service
	Name string `json:"name,omitempty"`

	// +kubebuilder:validation:Optional
	// Cinder Volume Container Image URL
	ContainerImage string `json:"containerImage,omitempty"`

	// +kubebuilder:validation:Required
	// +kubebuilder:default=1
	// Cinder Volume Replicas
	Replicas int32 `json:"replicas"`

	// Cinder Volume node selector
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// +kubebuilder:validation:Optional
	// +kubebuilder:default="# add your customization here"
	// CustomServiceConfig - customize the service config using this parameter to change service defaults,
	// or overwrite rendered information using raw OpenStack config format. The content gets added to
	// to /etc/<service>/<service>.conf.d directory as custom.conf file.
	CustomServiceConfig string `json:"customServiceConfig,omitempty"`
}

// CinderStatus defines the observed state of Cinder
type CinderStatus struct {
	// Map of hashes to track e.g. job status
	Hash map[string]string `json:"hash,omitempty"`

	// Conditions
	Conditions condition.List `json:"conditions,omitempty" optional:"true"`

	// Cinder Database Hostname
	DatabaseHostname string `json:"databaseHostname,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// Cinder is the Schema for the cinders API
type Cinder struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CinderSpec   `json:"spec,omitempty"`
	Status CinderStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// CinderList contains a list of Cinder
type CinderList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Cinder `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Cinder{}, &CinderList{})
}
