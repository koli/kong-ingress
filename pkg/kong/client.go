package kong

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

type CoreInterface interface {
	RESTClient() rest.Interface

	APIGetter
	PluginGetter
}

// CoreClient is used to interact with features provided by the Core group.
type CoreClient struct {
	restClient rest.Interface
}

// RESTClient returns a RESTClient that is used to communicate
// with API server by this client implementation.
func (c *CoreClient) RESTClient() rest.Interface {
	if c == nil {
		return nil
	}
	return c.restClient
}

func (c *CoreClient) Plugin(apiNameOrID string) PluginInterface {
	return newPlugin(c, apiNameOrID)
}

func (c *CoreClient) API() APIInterface {
	return newAPI(c)
}

func newAPI(c *CoreClient) *apiKong {
	return &apiKong{
		client: c.RESTClient(),
		resource: &metav1.APIResource{
			Name:       "apis",
			Namespaced: false,
		},
	}
}

func newPlugin(c *CoreClient, nameOrID string) *plugin {
	return &plugin{
		client: c.RESTClient(),
		resource: &metav1.APIResource{
			Name:       "apis",
			Namespaced: false,
		},
		nameOrID: nameOrID,
	}
}

// NewKongRESTClient generates a new *rest.Interface to communicate with the Kong Admin API
func NewKongRESTClient(c *rest.Config) (*CoreClient, error) {
	// c.APIPath = "/apis"
	c.ContentConfig = dynamic.ContentConfig()
	cl, err := rest.UnversionedRESTClientFor(c)
	if err != nil {
		return nil, err
	}
	return &CoreClient{restClient: cl}, nil
}
