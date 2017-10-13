package kong

import (
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type Domain struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DomainSpec   `json:"spec"`
	Status DomainStatus `json:"status"`
}

type DomainList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []Domain `json:"items"`
}

// DomainStatus represents information about the status of a domain.
type DomainStatus struct {
	// The state of the domain, an empty state means it's a new resource
	// +optional
	Phase DomainPhase `json:"phase,omitempty"`
	// A human readable message indicating details about why the domain claim is in this state.
	// +optional
	Message string `json:"message,omitempty"`
	// A brief CamelCase message indicating details about why the domain claim is in this state. e.g. 'AlreadyClaimed'
	// +optional
	Reason string `json:"reason,omitempty"`
	// The last time the resource was updated
	LastUpdateTime time.Time `json:"lastUpdateTime,omitempty"`
	// DeletionTimestamp it's a temporary field to work around the issue:
	// https://github.com/kubernetes/kubernetes/issues/40715, once it's solved,
	// remove this field and use the DeletionTimestamp from metav1.ObjectMeta
	DeletionTimestamp *metav1.Time `json:"deletionTimestamp,omitempty"`
}

// DomainSpec represents information about a domain claim
type DomainSpec struct {
	// PrimaryDomain is the name of the primary domain, to set the resource as primary,
	// 'name' and 'primary' must have the same value.
	// +required
	PrimaryDomain string `json:"primary"`
	// Sub is the label of the Primary Domain to form a subdomain
	// +optional
	Sub string `json:"sub,omitempty"`
	// Delegates contains a list of namespaces that are allowed to use this domain.
	// New domain resources could be referenced to primary ones using the 'parent' key.
	// A wildcard ("*") allows delegate access to all namespaces in the cluster.
	// +optional
	Delegates []string `json:"delegates,omitempty"`
	// Parent refers to the namespace where the primary domain is in.
	// It only makes sense when the type of the domain is set to 'shared',
	// +optional
	Parent string `json:"parent,omitempty"`
}

const (
	Finalizer = "kolihub.io/kong"
)

// DomainPhase is a label for the condition of a domain at the current time.
type DomainPhase string

const (
	// DomainStatusNew means it's a new resource and the phase it's not set
	DomainStatusNew DomainPhase = ""
	// DomainStatusOK means the domain doesn't have no pending operations or prohibitions,
	// and new ingresses could be created using the target domain.
	DomainStatusOK DomainPhase = "OK"
	// DomainStatusPending indicates that a request to create a new domain
	// has been received and is being processed.
	DomainStatusPending DomainPhase = "Pending"
	// DomainStatusFailed means the resource has failed on claiming the domain
	DomainStatusFailed DomainPhase = "Failed"
)

// APIResponse contains the response from an API call
type APIResponse struct {
	err        error
	StatusCode int
	Raw        []byte
}

func (r *APIResponse) Error() error {
	return r.err
}

func (r *APIResponse) String() string {
	if r.Raw == nil && r.StatusCode == 0 {
		return r.err.Error()
	}
	if r.Raw != nil {
		return fmt.Sprintf("[%d] %s", r.StatusCode, string(r.Raw))
	}
	return fmt.Sprintf("[%d] %s", r.StatusCode, r.err)
}

type Timestamp int64

// Retrieve the creation timestamp in time.Time
func (t Timestamp) GetTime() time.Time {
	return time.Unix(int64(t/1000), 0)
}

// API represents a kong api object
// ref: https://getkong.org/docs/0.10.x/admin-api/#api-object
type API struct {
	UID          string    `json:"id,omitempty"`
	Name         string    `json:"name,omitempty"`
	Hosts        []string  `json:"hosts,omitempty"`
	URIs         []string  `json:"uris,omitempty"`
	PreserveHost bool      `json:"preserve_host"`
	UpstreamURL  string    `json:"upstream_url"`
	CreatedAt    Timestamp `json:"created_at,omitempty"`
}

// APIList is a list of API's
// ref: https://getkong.org/docs/0.10.x/admin-api/#list-apis
type APIList struct {
	Total    int    `json:"total"`
	Items    []API  `json:"data"`
	NextPage string `json:"next"`
	Offset   string `json:"offset"`
}

// PluginSchema holds the specification of a plugin
type PluginSchema map[string]interface{}

// PluginName is an installed plugin on kong
// ref: https://getkong.org/plugins/
type PluginName string

// https://getkong.org/docs/0.10.x/admin-api/#retrieve-enabled-plugins
const (
	// ref: https://getkong.org/plugins/cors/
	CorsPlugin PluginName = "cors"
	// ref: https://getkong.org/plugins/ip-restriction/
	IpRestrictionPlugin PluginName = "ip-restriction"
	// ref: https://getkong.org/plugins/dynamic-ssl/
	DynamicSSLPlugin PluginName = "ssl"
	// ref: https://getkong.org/plugins/rate-limiting/
	RateLimitingPlugin PluginName = "rate-limiting"
)

// Plugin represents a kong plugin object
// ref: https://getkong.org/docs/0.10.x/admin-api/#plugin-object
type Plugin struct {
	UID        string       `json:"id,omitempty"`
	APIUID     string       `json:"api_id,omitempty"`
	Name       PluginName   `json:"name"`
	ConsumerID string       `json:"consumer_id,omitempty"`
	Config     PluginSchema `json:"config"`
	Enabled    bool         `json:"enabled,omitempty"`
	CreatedAt  Timestamp    `json:"created_at,omitempty"`
}

// PluginList is a list of plugins
// ref: https://getkong.org/docs/0.10.x/admin-api/#list-all-plugins
type PluginList struct {
	Total    int      `json:"total"`
	Items    []Plugin `json:"data"`
	NextPage string   `json:"next"`
}

// KongVersion represents the semantic version of a kong node
type KongVersion struct {
	Major int
	Minor int
	Patch int
}

// String returns the string representation of the KongVersion struct
func (k *KongVersion) String() string {
	return fmt.Sprintf("v%d.%d.%d", k.Major, k.Minor, k.Patch)
}
