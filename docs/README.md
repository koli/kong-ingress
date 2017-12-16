# How it Works

The controller watches for ingress endpoints and provisions Kong routes based on the ingress spec. It's only allowed to do so when all the domains are claimed for each host found in the ingress resource. A negative response generates a warning as a Kubernetes Event.

Each host could have multiple paths endpoints with multiple services backends, the controller validates if a service exists for each path. The full URI of the service is used as the `upstream_url` when creating a route in Kong.

When an API is created by the controller it's identified following the convention: `[host]~[namespace]~[path-hash]`

- `host` is the full name of the domain
- `namespace` is the name of the current namespace of the resource
- `path-hash` is the URI part of a HTTPIngressPath hash encoded

The domain claims ensure that every route is created or updated only if the namespace owns the domains specified in the ingress resource, thus preventing overwriting existing routes which doesn't belong to a specific namespace. [Read more about domain claims](domain-claims.md)

#### Cleaning Up

An ingress resource could be updated anytime, the controller doesn't have a way to identify when a host is deleted or updated and process those changes to Kong. When the controller identifies an update, it tries to sync the current state of the ingress resource. Each backend is associated with a service and also with a domain, thus the resources are only cleaned when a Kubernetes service or the domain resource is deleted.

> **IMPORTANT:** Deleting/updating an ingress resource doesn't clean the Kong `apis`!

# Operation Modes

## Default Mode

The default mode of operation range through each host in an ingress resource validating if a domain claim exists with the [status OK.](domain-claims.md#Status). A domain claim need to be created manually for each host in an ingress resource.

The following spec ...

```yaml
apiVersion: platform.koli.io/v1
kind: Domain
metadata:
  name: acme-org
  namespace: acme
spec:
  primary: acme.org
```

Is required for the ingress resource below to work:

```yaml
apiVersion: extensions/v1beta1
kind: Ingress
metadata:
  name: acme-org
spec:
  rules:
  - host: acme.org
    http:
      paths:
      - path: /
        backend:
          serviceName: web
          servicePort: 80
```

## Auto Claim Mode

The `Auto Claim` mode the controller leases domains automatically for each host record in an Ingress resource, it proceeds to create the routes only after all the domains are claimed.

The following ingress resource ...

```yaml
apiVersion: extensions/v1beta1
kind: Ingress
metadata:
  name: acme-org
spec:
  rules:
  - host: acme.org
    http:
      paths:
      - path: /
        backend:
          serviceName: web
          servicePort: 80
```

Automatically generates the domain resource:

```yaml
apiVersion: platform.koli.io/v1
kind: Domain
metadata:
  creationTimestamp: 2017-04-27T21:11:39Z
  finalizers:
  - kolihub.io/kong
  name: acme-org
  namespace: default
  resourceVersion: "2386"
  selfLink: /apis/platform.koli.io/v1/namespaces/default/domains/acme-org
  uid: 1ad92d88-2b8e-11e7-8c48-a6fe4615cf32
spec:
  primary: acme.org
status:
  lastUpdateTime: 2017-04-27T21:11:39.381122526Z
  message: Primary domain claimed with success
  phase: OK
```

And creates the Kong API:

```json
{
  "uris": [
    "/"
  ],
  "id": "2cbe86b6-fab6-4997-bc08-e103bd7a452d",
  "upstream_read_timeout": 60000,
  "preserve_host": false,
  "created_at": 1493327500000,
  "upstream_connect_timeout": 60000,
  "upstream_url": "http://web.default.svc.cluster.local",
  "strip_uri": true,
  "name": "acme.org~default~300030",
  "https_only": false,
  "http_if_terminated": true,
  "retries": 5,
  "upstream_send_timeout": 60000,
  "hosts": [
    "acme.org"
  ]
}
```

### Configuring Domain Claims in ingress resources

#### Explicity set as primary

If the host has only 2 segments (e.g.: domain.tld) a domain resource is created as primary, if it has 3 or more segments is automatically created as shared using the first segment as the `sub` attribute (e.g.: app.domain.tld).
To set a host that has 2 or more segments as primary, an annotation is required:

```yaml
apiVersion: extensions/v1beta1
kind: Ingress
metadata:
  name: coyote-acme-org
  annotations:
    # set the specified host as primary
    kolihub.io/coyote.acme.org: primary
spec:
  rules:
  # this host will generate a domain claim as primary!
  - host: coyote.acme.org
    http:
      paths:
      - path: /
        backend:
          serviceName: web
          servicePort: 80
```

#### Configure a domain as parent

A parent namespace configured in an ingress creates all domain claims with the attribute `parent`, an annotation is required:

```yaml
apiVersion: extensions/v1beta1
kind: Ingress
metadata:
  name: coyote-acme-org
  annotations:
    # lookup for a primary domain in the namespace 'acme-project'
    # the target namespace must allow using the attribute `delegates`
    kolihub.io/parent: acme-project
spec:
  rules:
  - host: coyote.acme.org
    http:
      paths:
      - path: /
        backend:
          serviceName: web
          servicePort: 80
```

### Configure a set of API Objects via Ingress Resource Annotations
A set of API Objects managed and exposed by Kong can be configured by annotating Ingress resources.

#### Configurating the `strip_uri` property
The [`strip_uri`](https://getkong.org/docs/0.10.x/proxy/#the-strip_uri-property) property determines whether or not to strip a matching prefix from the upstream URI to be requested. The default value is `true` but it might not be the desire behavior in all cases. To set the `strip_uri` property for a set of APIs an annotation is required.

```yaml
apiVersion: extensions/v1beta1
kind: Ingress
metadata:
  name: coyote-acme-org
  annotations:
    ingress.kubernetes.io/strip-uri: "false"
spec:
  rules:
  # this host will generate a domain claim as primary!
  - host: coyote.acme.org
    http:
      paths:
      - path: /
        backend:
          serviceName: web
          servicePort: 80
```

#### Configurating the `preserve_host` property
When proxying, Kong's default behavior is to set the upstream request's Host header to the hostname of the API's upstream_url property. The [`preserve_host`](https://getkong.org/docs/0.10.x/proxy/#the-preserve_host-property) field accepts a boolean flag instructing Kong not to do so. Default value is false.
