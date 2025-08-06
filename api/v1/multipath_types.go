package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type MultipathSpec struct {
	Config          MultipathConfig       `json:"config,omitempty"`
	NodeSelector    map[string]string     `json:"nodeSelector,omitempty"`
	UpdateStrategy  UpdateStrategy        `json:"updateStrategy,omitempty"`
	SecurityContext SecurityContextConfig `json:"securityContext,omitempty"`
}

type MultipathConfig struct {
	Defaults            DefaultsConfig       `json:"defaults,omitempty"`
	Blacklist           []BlacklistEntry     `json:"blacklist,omitempty"`
	BlacklistExceptions []BlacklistException `json:"blacklistExceptions,omitempty"`
	Multipaths          []MultipathEntry     `json:"multipaths,omitempty"`
	Devices             []DeviceEntry        `json:"devices,omitempty"`
}

type DefaultsConfig struct {
	UserFriendlyNames  string `json:"user_friendly_names,omitempty"`
	FindMultipaths     string `json:"find_multipaths,omitempty"`
	PathGroupingPolicy string `json:"path_grouping_policy,omitempty"`
	PathSelector       string `json:"path_selector,omitempty"`
	PathChecker        string `json:"path_checker,omitempty"`
	Failback           string `json:"failback,omitempty"`
	RRWeight           string `json:"rr_weight,omitempty"`
	NoPathRetry        string `json:"no_path_retry,omitempty"`
	RRMinIO            string `json:"rr_min_io,omitempty"`
	FlushOnLastDel     string `json:"flush_on_last_del,omitempty"`
	MaxFDS             string `json:"max_fds,omitempty"`
	CheckerTimeout     string `json:"checker_timeout,omitempty"`
	FastIOFailTmo      string `json:"fast_io_fail_tmo,omitempty"`
	DevLossTimeout     string `json:"dev_loss_timeout,omitempty"`
}

type BlacklistEntry struct {
	Devnode string           `json:"devnode,omitempty"`
	WWID    string           `json:"wwid,omitempty"`
	Device  DeviceIdentifier `json:"device,omitempty"`
}

type BlacklistException struct {
	Devnode string           `json:"devnode,omitempty"`
	WWID    string           `json:"wwid,omitempty"`
	Device  DeviceIdentifier `json:"device,omitempty"`
}

type DeviceIdentifier struct {
	Vendor  string `json:"vendor,omitempty"`
	Product string `json:"product,omitempty"`
}

type MultipathEntry struct {
	WWID               string `json:"wwid"`
	Alias              string `json:"alias,omitempty"`
	PathGroupingPolicy string `json:"path_grouping_policy,omitempty"`
	PathSelector       string `json:"path_selector,omitempty"`
	Failback           string `json:"failback,omitempty"`
}

type DeviceEntry struct {
	Vendor             string `json:"vendor"`
	Product            string `json:"product"`
	PathGroupingPolicy string `json:"path_grouping_policy,omitempty"`
	PathSelector       string `json:"path_selector,omitempty"`
	PathChecker        string `json:"path_checker,omitempty"`
	Features           string `json:"features,omitempty"`
	HardwareHandler    string `json:"hardware_handler,omitempty"`
	Failback           string `json:"failback,omitempty"`
	RRWeight           string `json:"rr_weight,omitempty"`
	NoPathRetry        string `json:"no_path_retry,omitempty"`
	RRMinIO            string `json:"rr_min_io,omitempty"`
	FastIOFailTmo      string `json:"fast_io_fail_tmo,omitempty"`
	DevLossTimeout     string `json:"dev_loss_timeout,omitempty"`
}

type UpdateStrategy struct {
	Type          string                 `json:"type,omitempty"`
	RollingUpdate *RollingUpdateStrategy `json:"rollingUpdate,omitempty"`
}

type RollingUpdateStrategy struct {
	MaxUnavailable *int32 `json:"maxUnavailable,omitempty"`
}

type SecurityContextConfig struct {
	Privileged               *bool           `json:"privileged,omitempty"`
	AllowPrivilegeEscalation *bool           `json:"allowPrivilegeEscalation,omitempty"`
	RunAsUser                *int64          `json:"runAsUser,omitempty"`
	RunAsGroup               *int64          `json:"runAsGroup,omitempty"`
	SELinuxOptions           *SELinuxOptions `json:"seLinuxOptions,omitempty"`
}

type SELinuxOptions struct {
	Level string `json:"level,omitempty"`
	Role  string `json:"role,omitempty"`
	Type  string `json:"type,omitempty"`
	User  string `json:"user,omitempty"`
}

type MultipathStatus struct {
	Phase      string             `json:"phase,omitempty"`
	Ready      bool               `json:"ready"`
	Message    string             `json:"message,omitempty"`
	NodesReady int32              `json:"nodesReady"`
	NodesTotal int32              `json:"nodesTotal"`
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="Ready",type="boolean",JSONPath=".status.ready"
// +kubebuilder:printcolumn:name="Nodes Ready",type="integer",JSONPath=".status.nodesReady"
// +kubebuilder:printcolumn:name="Nodes Total",type="integer",JSONPath=".status.nodesTotal"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

type Multipath struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MultipathSpec   `json:"spec,omitempty"`
	Status MultipathStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

type MultipathList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Multipath `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Multipath{}, &MultipathList{})
}
