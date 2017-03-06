package kong

import (
	"encoding/json"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/rest"
)

// ApiGetter has a method to return an ApiInterface
// A group's client should implement this interface.
type APIGetter interface {
	API() APIInterface
}

// APIInterface has methods to work with Kong Endpoints
// ref: https://getkong.org/docs/0.9.x/admin-api/#api-object
type APIInterface interface {
	List(selector fields.Selector) (*APIList, error)
	Get(name string) (*API, error)
	UpdateOrCreate(data *API) (*API, error)
	Delete(nameOrID string) error
}

type api struct {
	client   rest.Interface
	resource *metav1.APIResource
}

// Get gets the resource with the specified name.
func (a *api) Get(name string) (*API, error) {
	api := &API{}
	data, err := a.client.Get().
		Resource(a.resource.Name).
		Name(name).
		DoRaw()
	if err != nil {
		return nil, err
	}
	return api, json.Unmarshal(data, api)
}

// List returns a list of objects for this resource.
func (a *api) List(selector fields.Selector) (*APIList, error) {
	apiList := &APIList{}
	data, err := a.client.Get().
		Resource(a.resource.Name).
		FieldsSelectorParam(selector). // TOD: test it
		DoRaw()
	if err != nil {
		return nil, err
	}
	return apiList, json.Unmarshal(data, apiList)
}

// Delete deletes the resource with the specified name.
func (a *api) Delete(nameOrID string) error {
	return a.client.Delete().
		Resource(a.resource.Name).
		Name(nameOrID).
		Do().
		Error()
}

// Update updates the provided resource.
func (a *api) UpdateOrCreate(data *API) (*API, error) {
	rawData, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	resp, err := a.client.Put().
		Resource(a.resource.Name).
		Body(rawData).
		SetHeader("Content-Type", "application/json").
		DoRaw()
	if err != nil {
		return nil, err
	}
	api := &API{}
	return api, json.Unmarshal(resp, api)
}
