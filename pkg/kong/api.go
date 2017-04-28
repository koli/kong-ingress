package kong

import (
	"encoding/json"
	"reflect"

	"net/url"

	"regexp"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

// APIGetter has a method to return an ApiInterface
// A group's client should implement this interface.
type APIGetter interface {
	API() APIInterface
}

// APIInterface has methods to work with Kong Endpoints
// ref: https://getkong.org/docs/0.9.x/admin-api/#api-object
type APIInterface interface {
	List(params url.Values) (*APIList, error)
	ListByRegexp(params url.Values, pattern string) (*APIList, error)
	Get(name string) (*API, *APIResponse)
	UpdateOrCreate(data *API) (*API, *APIResponse)
	Delete(nameOrID string) error
}

type apiKong struct {
	client   rest.Interface
	resource *metav1.APIResource
}

// Get gets the resource with the specified name.
func (a *apiKong) Get(name string) (*API, *APIResponse) {
	api := &API{}
	resp := a.client.Get().
		Resource(a.resource.Name).
		Name(name).
		Do()
	statusCode := reflect.ValueOf(resp).FieldByName("statusCode").Int()
	raw, err := resp.Raw()
	response := &APIResponse{StatusCode: int(statusCode), err: err}
	if err != nil {
		response.Raw = raw
		return nil, response
	}
	response.err = json.Unmarshal(raw, api)
	return api, response
}

// List returns a list of objects for this resource.
func (a *apiKong) List(params url.Values) (*APIList, error) {
	apiList := &APIList{}
	request := a.client.Get().Resource(a.resource.Name)
	for k, vals := range params {
		for _, v := range vals {
			request.Param(k, v)
		}
	}
	data, err := request.DoRaw()
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, apiList); err != nil {
		return nil, err
	}
	// recurse to retrieve all apis
	if len(apiList.NextPage) > 0 {
		params.Add("offset", apiList.Offset)
		result, err := a.List(params)
		if err != nil {
			return nil, err
		}
		apiList.Items = append(apiList.Items, result.Items...)
	}
	return apiList, err
}

// ListByRegexp returns a list of object for this resource based on a regexp pattern
// which is evaluated by regexp.MustCompile
func (a *apiKong) ListByRegexp(params url.Values, pattern string) (*APIList, error) {
	apiList, err := a.List(params)
	if err != nil {
		return nil, err
	}
	apiListResult := &APIList{}
	re := regexp.MustCompile(pattern)
	for _, api := range apiList.Items {
		if re.MatchString(api.Name) {
			apiListResult.Items = append(apiListResult.Items, api)
		}
	}
	return apiListResult, nil
}

// Delete deletes the resource with the specified name.
func (a *apiKong) Delete(nameOrID string) error {
	return a.client.Delete().
		Resource(a.resource.Name).
		Name(nameOrID).
		Do().
		Error()
}

// Update updates the provided resource.
func (a *apiKong) UpdateOrCreate(data *API) (*API, *APIResponse) {
	rawData, err := json.Marshal(data)
	if err != nil {
		return nil, &APIResponse{err: err}
	}
	resp := a.client.Put().
		Resource(a.resource.Name).
		Body(rawData).
		SetHeader("Content-Type", "application/json").
		Do()

	statusCode := reflect.ValueOf(resp).FieldByName("statusCode").Int()
	raw, err := resp.Raw()
	response := &APIResponse{StatusCode: int(statusCode), err: err}

	if err != nil {
		response.Raw = raw
		return nil, response
	}
	api := &API{}
	response.err = json.Unmarshal(raw, api)
	return api, response
}
