// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// PMTUNodeRelaySpec carries one captured ICMP frag-needed packet to be injected
// by peer nodes.
type PMTUNodeRelaySpec struct {
	// SourceNode is the name of the node that captured the packet.
	// +kubebuilder:validation:MaxLength=253
	SourceNode string `json:"sourceNode"`
	// Payload is the base64-encoded raw IP packet (ICMP type 3 code 4).
	// +kubebuilder:validation:MaxLength=2048
	Payload string `json:"payload"`
	// ExpiresAt is when this object may be garbage-collected if not yet consumed.
	ExpiresAt metav1.Time `json:"expiresAt"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,shortName=pnr
// +kubebuilder:printcolumn:name="Source",type=string,JSONPath=`.spec.sourceNode`
// +kubebuilder:printcolumn:name="Expires",type=string,JSONPath=`.spec.expiresAt`

// PMTUNodeRelay relays an ICMP frag-needed packet between nodes via the API server.
type PMTUNodeRelay struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              PMTUNodeRelaySpec `json:"spec"`
}

// +kubebuilder:object:root=true

// PMTUNodeRelayList is a list of PMTUNodeRelay.
type PMTUNodeRelayList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PMTUNodeRelay `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PMTUNodeRelay{}, &PMTUNodeRelayList{})
}
