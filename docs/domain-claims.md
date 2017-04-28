# Domain Claims

Domain Claims are needed to control who owns a specific host in the cluster, the third party resource below is used to represent the information about claims:

```yaml
apiVersion: extensions/v1beta1
kind: ThirdPartyResource
metadata:
  name: domain.platform.koli.io
description: Holds information about domain claims to prevent duplicated hosts in ingress resources
versions:
- name: v1
```

A domain claim is represented with the following specification:

```yaml
apiVersion: platform.koli.io/v1
kind: Domain
metadata:
  name: <metadata-name>
spec:
  primary: <domain.tld>
  sub: <subdomain>
  parent: <namespace>
  delegates:
    - <namespace01>
    - <namespace02>
    - (...)
```

# Spec

A domain could have two types: `primary` or `shared`.

## Primary

The `primary` represents the name of the primary domain, which could be used to lease domains to other namespaces or to configure routes on ingress resources. A primary domain is usually your main domain name, e.g.: `acme.org`, the specification below represents a primary domain:

```yaml
apiVersion: platform.koli.io/v1
kind: Domain
metadata:
  name: acme
  namespace: acme-org
spec:
  primary: acme.org
```

## Shared

A `shared` is a subdomain and means the domain is inherit from a `primary` type. When a `shared` domain is created the controller tries to search in three (3) namespaces following the order below:

1) Search in the `parent` attribute (must be a valid namespace)
2) Search in the `shared` domain resource namespace
3) Search in the [system namespace]()

If a `primary` domain couldn't be found, the resource is configured to a failing state and it will be retried until a `primary` be found.

The following specification represents a `shared` domain:

```yaml
apiVersion: platform.koli.io/v1
kind: Domain
metadata:
  name: coyote-acme
  namespace: acme-org
spec:
  primary: acme.org
  sub: coyote
```

> **Note:** A `shared` domain couldn't delegate domains.
> The `sub` attribute couldn't represent subdomains, e.g.: `sub: wile.coyote`.

## Parent

A `parent` attribute it's only useful when the resource is a `shared` type. It indicates the namespace to search for the `primary` domain, if it fail, fallbacks searching in the namespace of the resource and in [system namespace]()

> The `parent` namespace must explicity allow using the attribute `delegates`

The following specification represents a `shared` domain indicating a parent:

```yaml
apiVersion: platform.koli.io/v1
kind: Domain
metadata:
  name: coyote-acme
  namespace: coyote-org
spec:
  primary: acme.org
  sub: coyote
  parent: acme-org
```

## Delegates

A `delegates` attribute is only valid if the domain is `primary`. It indicates which namespaces could claim `shared` domains from it, a wildcard string ('*') means that all namespaces in the cluster could claim subdomains from the `primary`.

The following specification represents a `primary` domain delegating access to namespaces `coyote-org` and `marvin-org`:

```yaml
apiVersion: platform.koli.io/v1
kind: Domain
metadata:
  name: acme
  namespace: acme-org
spec:
  primary: acme.org
  delegates: 
    - coyote-org
    - marvin-org
```

The specification below represents a `shared` domain claiming from its `parent`:

```yaml
apiVersion: platform.koli.io/v1
kind: Domain
metadata:
  name: marvin-acme
  namespace: marvin-org
spec:
  primary: acme.org
  sub: marvin
  parent: acme-org
```

# Status

When a new domain claim is created the controller begins the provisioning. The `status` attribute indicates the state of the result of the claim. 

```yaml
apiVersion: platform.koli.io/v1
kind: Domain
metadata:
  name: marvin-acme
spec:
  primary: acme.org
  sub: marvin
  parent: acme-org
status:
  phase: Failed
  message: The primary domain wasn't found"
  reason: DomainNotFound
  lastUpdateTime: 2017-04-04T12:25:42Z
```

## New

The resource is prepared to be provisioned, in this state the kubernetes finalizer `kolihub.io/kong` is set and the status is changed to `Pending`. The status is represented by an empty string: `""`

> **Note:** The finalizer doesn't do anything at this time, because the implementation is broken in Kubernetes: https://github.com/kubernetes/kubernetes/issues/40715.
> In the future it will be used to clean the associated resources more efficiently (already implemented).

## Pending

The `Pending` state means the controller is searching for duplicates or inconsistencies.

- If it's a `primary` domain, search if exists a registered domain with that name on the cluster
- If it's a `shared` domain, search for a `primary` domain following the order:
  - In the `parent` namespace if it's specified
  - In the resource namespace
  - In the `system namespace`

If the domain doesn't contain any inconsistencies or duplicates, the state of the resource is set to `OK`.

## OK

The domain is ready to be used in a ingress resource.

## Failed

This state means the claim has failed, the details are described in `reason` and `message` attributes.

> **Note about status:** The status spec from a domain resource is used to control the state of a domain, the controller will act accordingly to this information.
> The `status` attributes aren't immutable, thus an user could change it causing an undesirable behaviour for the resource.
> [Related issue.](https://github.com/kubernetes/kubernetes/issues/38113)
