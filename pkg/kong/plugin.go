package kong

import (
	"encoding/json"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/rest"
)

// PluginGetter has a method to return an PluginInterface
// A group's client should implement this interface.
type PluginGetter interface {
	Plugin() PluginInterface
}

// PluginInterface has methods to work with Kong Endpoints
// ref: https://getkong.org/docs/0.9.x/admin-api/#api-object
type PluginInterface interface {
	List(selector fields.Selector) (*PluginList, error)
	Get(pluginID string) (*Plugin, error)
	UpdateOrCreate(data *Plugin) (*Plugin, error)
	Delete(pluginID string) error
}

// plugin implements PluginInterface
type plugin struct {
	client   rest.Interface
	resource *metav1.APIResource
	nameOrID string
}

// Get gets the resource with the specified name.
func (p *plugin) Get(pluginID string) (*Plugin, error) {
	data, err := p.client.Get().
		Resource(p.resource.Name).
		Name(p.nameOrID).
		SubResource("plugins").
		Suffix(pluginID).
		DoRaw()
	if err != nil {
		return nil, err
	}
	pl := &Plugin{}
	return pl, json.Unmarshal(data, pl)
}

// List returns a list of objects for this resource.
func (p *plugin) List(selector fields.Selector) (*PluginList, error) {
	data, err := p.client.Get().
		Resource(p.resource.Name).
		Name(p.nameOrID).
		SubResource("plugins").
		DoRaw()
	if err != nil {
		return nil, err
	}
	pluginList := &PluginList{}
	return pluginList, json.Unmarshal(data, pluginList)
}

// Update updates the provided resource.
func (p *plugin) UpdateOrCreate(data *Plugin) (*Plugin, error) {
	rawData, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.Put().
		Resource(p.resource.Name).
		Name(p.nameOrID).
		SubResource("plugins").
		Body(rawData).
		SetHeader("Content-Type", "application/json").
		DoRaw()
	if err != nil {
		return nil, err
	}
	pl := &Plugin{}
	return pl, json.Unmarshal(resp, pl)
}

// Delete deletes the resource with the specified name.
func (p *plugin) Delete(pluginID string) error {
	return p.client.Delete().
		Resource(p.resource.Name).
		Name(p.nameOrID).
		SubResource("plugins").
		Suffix(pluginID).
		Do().
		Error()
}
