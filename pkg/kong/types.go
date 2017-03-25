package kong

import (
	"fmt"
	"time"
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

type timestamp int64

// Retrieve the creation timestamp in time.Time
func (t timestamp) GetTime() time.Time {
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
	CreatedAt    timestamp `json:"created_at,omitempty"`
}

// APIList is a list of API's
// ref: https://getkong.org/docs/0.10.x/admin-api/#list-apis
type APIList struct {
	Total    int    `json:"total"`
	Items    []API  `json:"data"`
	NextPage string `json:"next"`
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
	CreatedAt  timestamp    `json:"created_at,omitempty"`
}

// PluginList is a list of plugins
// ref: https://getkong.org/docs/0.10.x/admin-api/#list-all-plugins
type PluginList struct {
	Total    int      `json:"total"`
	Items    []Plugin `json:"data"`
	NextPage string   `json:"next"`
}
