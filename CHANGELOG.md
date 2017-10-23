# v0.3.1-alpha

**Image:** `quay.io/koli/kong-ingress:v0.3.1-alpha`

## Changes

- Configure Strip URI parameter for a set of API Objects via Ingress Annotations, by @paniagua

# v0.3.0-alpha

**Image:** `quay.io/koli/kong-ingress:v0.3.0-alpha`

## BREAKING CHANGE

Third Party Resources are deprecated in v1.7.0 and removed in v1.8.0, this release depreciate TPR and includes Custom Resource Definition, thus it's important to migrate all the domain resources. See this [article](https://kubernetes.io/docs/tasks/access-kubernetes-api/migrate-third-party-resource/) to understand how this migration works.

## Changes

- Update kubernetes library to the latest release

# v0.2.1-alpha

**Image:** `quay.io/koli/kong-ingress:v0.2.1-alpha`

## Added

- Add support for custom ports on ingress [#17](https://github.com/kolihub/kong-ingress/issues/17)

# v0.2.0-alpha

**Image:** `quay.io/koli/kong-ingress:v0.2.0-alpha`

## Features

- Expose applications through domain names
- Host collision prevention
- Delegate subdomains throughout a kubernetes cluster
- Path based routing 
