/*
Copyright 2021 The KCP Authors.

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

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	conditionsv1alpha1 "github.com/kcp-dev/kcp/pkg/apis/third_party/conditions/apis/conditions/v1alpha1"
	"github.com/kcp-dev/kcp/pkg/apis/third_party/conditions/util/conditions"
)

// SyncTarget describes a member cluster capable of running workloads.
//
// +crd
// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,categories=kcp
// +kubebuilder:printcolumn:name="Location",type="string",JSONPath=`.metadata.name`,priority=1
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=`.status.conditions[?(@.type=="Ready")].status`,priority=2
// +kubebuilder:printcolumn:name="Synced API resources",type="string",JSONPath=`.status.syncedResources`,priority=3
type SyncTarget struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec holds the desired state.
	// +optional
	Spec SyncTargetSpec `json:"spec,omitempty"`

	// Status communicates the observed state.
	// +optional
	Status SyncTargetStatus `json:"status,omitempty"`
}

var _ conditions.Getter = &SyncTarget{}
var _ conditions.Setter = &SyncTarget{}

// SyncTargetSpec holds the desired state of the SyncTarget (from the client).
type SyncTargetSpec struct {
	// Unschedulable controls cluster schedulability of new workloads. By
	// default, cluster is schedulable.
	// +optional
	// +kubebuilder:default=false
	Unschedulable bool `json:"unschedulable"`

	// EvictAfter controls cluster schedulability of new and existing workloads.
	// After the EvictAfter time, any workload scheduled to the cluster
	// will be unassigned from the cluster.
	// By default, workloads scheduled to the cluster are not evicted.
	EvictAfter *metav1.Time `json:"evictAfter,omitempty"`
}

// SyncTargetStatus communicates the observed state of the SyncTarget (from the controller).
type SyncTargetStatus struct {

	// Allocatable represents the resources that are available for scheduling.
	// +optional
	Allocatable *corev1.ResourceList `json:"allocatable,omitempty"`

	// Capacity represents the total resources of the cluster.
	// +optional
	Capacity *corev1.ResourceList `json:"capacity,omitempty"`

	// Current processing state of the SyncTarget.
	// +optional
	Conditions conditionsv1alpha1.Conditions `json:"conditions,omitempty"`

	// +optional
	SyncedResources []string `json:"syncedResources,omitempty"`

	// A timestamp indicating when the syncer last reported status.
	// +optional
	LastSyncerHeartbeatTime *metav1.Time `json:"lastSyncerHeartbeatTime,omitempty"`

	// VirtualWorkspaces contains all syncer virtual workspace URLs.
	// +optional
	VirtualWorkspaces []VirtualWorkspace `json:"virtualWorkspaces,omitempty"`
}

type VirtualWorkspace struct {
	// URL is the URL of the syncer virtual workspace.
	//
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:format:URL
	// +required
	URL string `json:"url"`
}

// SyncTargetList is a list of SyncTarget resources
//
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type SyncTargetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []SyncTarget `json:"items"`
}

// Conditions and ConditionReasons for the kcp SyncTarget object.
const (
	// SyncerReady means the syncer is ready to transfer resources between KCP and the SyncTarget.
	SyncerReady conditionsv1alpha1.ConditionType = "SyncerReady"

	// APIImporterReady means the APIImport component is ready to import APIs from the SyncTarget.
	APIImporterReady conditionsv1alpha1.ConditionType = "APIImporterReady"

	// HeartbeatHealthy means the HeartbeatManager has seen a heartbeat for the SyncTarget within the expected interval.
	HeartbeatHealthy conditionsv1alpha1.ConditionType = "HeartbeatHealthy"

	// SyncTargetUnknownReason documents a SyncTarget which readiness is unknown.
	SyncTargetUnknownReason = "SyncTargetStatusUnknown"

	// SyncTargetReadyReason documents a SyncTarget that is ready.
	SyncTargetReadyReason = "SyncTargetReady"

	// SyncTargetNotReadyReason documents a SyncTarget is not ready, when the "readyz" check returns false.
	SyncTargetNotReadyReason = "SyncTargetNotReady"

	// SyncTargetUnreachableReason documents the SyncTarget state when the Syncer is unable to reach the SyncTarget "readyz" API endpoint
	SyncTargetUnreachableReason = "SyncTargetUnreachable"

	// ErrorStartingSyncerReason indicates that the Syncer failed to start.
	ErrorStartingSyncerReason = "ErrorStartingSyncer"

	// ErrorInstallingSyncerReason indicates that the Syncer failed to install.
	ErrorInstallingSyncerReason = "ErrorInstallingSyncer"

	// InvalidKubeConfigReason indicates that the Syncer failed to start because the KubeConfig is invalid.
	InvalidKubeConfigReason = "InvalidKubeConfig"

	// ErrorCreatingClientReason indicates that there has been an error trying to create a kubernetes client from given a KubeConfig.
	ErrorCreatingClientReason = "ErrorCreatingClient"

	// ErrorStartingAPIImporterReason indicates an error starting the API Importer.
	ErrorStartingAPIImporterReason = "ErrorStartingAPIImporter"

	// ErrorHeartbeatMissedReason indicates that a heartbeat update was not received within the configured threshold.
	ErrorHeartbeatMissedReason = "ErrorHeartbeat"
)

func (in *SyncTarget) SetConditions(conditions conditionsv1alpha1.Conditions) {
	in.Status.Conditions = conditions
}

func (in *SyncTarget) GetConditions() conditionsv1alpha1.Conditions {
	return in.Status.Conditions
}
