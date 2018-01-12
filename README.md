# Kong Ingress

[![Build Status](https://travis-ci.org/koli/kong-ingress.svg?branch=master)](https://travis-ci.org/koli/kong-ingress)

It's a Kubernetes [Ingress](https://kubernetes.io/docs/concepts/services-networking/ingress/) Controller for [Kong](https://getkong.org/about) which manages Kong apis for each existent host on ingresses resources.

# What's an Ingress Controller

An Ingress Controller is a daemon, deployed as a Kubernetes Pod, that watches the apiserver's /ingresses endpoint for updates to the Ingress resource. Its job is to satisfy requests for ingress.

# Important Note

- This is a work in progress project.
- It relies on a beta Kubernetes resource.

# Overview

Kong it's an API Gateway that deals with L7 traffic, the ingress uses the kong admin API for managing the [apis resources](https://getkong.org/docs/0.10.x/admin-api/#api-object). Each existent host on an ingress spec could map several apis on Kong enabling path based routing. The main object of this controller is to act as an orchestrator of domains and routes on Kong. Load balancing between containers could be achieved using [Services](https://kubernetes.io/docs/concepts/services-networking/service/). To expose your routes outside of the cluster, choose between a [publish service type](https://kubernetes.io/docs/concepts/services-networking/service/#publishing-services---service-types) on Kubernetes.

## Domain Claims

Some of the main problems of using name based virtual hosting with ingress is that you can't know who's the owner of a specific host, thus a Kong api could be updated by multiple ingress resources resulting in an unwanted behaviour.

A [Custom Resource Definition](https://kubernetes.io/docs/concepts/api-extension/custom-resources/) is used to allow the kong ingress to lease domains for each host specified on ingress resources. If a domain is already claimed in the cluster, the controller rejects the creation of apis on Kong.

Read more about [Domain Claims.](./docs/domain-claims.md)

> In the future this probally will change if the Ingress Claim Proposal move forward.

**More Info:**

- [Domain Claims](./docs/domain-claims.md)
- [Ingress Claim Proposal](https://docs.google.com/document/d/1Kj9OcTQdERZgNkZhdDxnQeT-TI4DLqqg62lShnboT6s/)
- [Kubernetes GitHub Issue](https://github.com/kubernetes/kubernetes/issues/30151)

## Controller Scope

The controller watches for all ingress resources of the cluster, meaning that it's not necessary to install multiple instances of the controller by namespace.

## Prerequisites

- Kubernetes cluster v1.7.0+
- Kubernetes DNS add-on
- Kong server v0.10.0+

# Quick Start - Minikube

> The example above installs Kong and the Ingress Controller in the `default` namespace. It's recommended to install the components in a custom namespace to facilitate administration.

1) **Follow the [Kong Kubernetes Tutorial](https://getkong.org/install/kubernetes/) to install a Kubernetes cluster with Kong**
2) **Install RBAC (optional)**

If RBAC is in place, users must create RBAC rules for the ingress controller:

```bash
kubectl create -f ./examples/rbac/cluster-role.yaml
kubectl create -f ./examples/rbac/cluster-role-binding.yaml
```

> It will enable access only to the default Service Account and only to the required resources.
> **Note:** The cluster role binding namespace defaults to `kong-system`, make sure to change
> if you're installing in a different namespace.

3) **Install the Kong Ingress Controller**


```bash
kubectl create -f ./examples/deployment.yaml
```

After all pods are in the `Running` state, begin to create your routes. The example above creates two distinct deployments and expose then using services as `web` and `hello`:

```bash
# An example app
kubectl create -f - <<EOF
apiVersion: extensions/v1beta1
kind: Deployment
metadata:
  name: web
spec:
  replicas: 2
  template:
    metadata:
      labels:
        app: web
    spec:
      containers:
      - name: web
        image: nginx:1.7.9
        ports:
        - containerPort: 80
---
apiVersion: extensions/v1beta1
kind: Deployment
metadata:
  name: hello
spec:
  replicas: 1
  template:
    metadata:
      labels:
        app: hello
    spec:
      containers:
      - name: hello
        image: tutum/hello-world
        ports:
        - containerPort: 80
---
apiVersion: v1
kind: Service
metadata:
  name: web
spec:
  ports:
    - port: 80
      targetPort: 80
      protocol: TCP
      name: http
  selector:
    app: web
---
apiVersion: v1
kind: Service
metadata:
  name: hello
spec:
  ports:
    - port: 80
      targetPort: 80
      protocol: TCP
      name: http
  selector:
    app: hello
EOF
```

4) **The ingress resource below will create 4 routes at Kong, one route for each path**

```bash
# The ingress resource mapping the routes
kubectl create -f - <<EOF
apiVersion: extensions/v1beta1
kind: Ingress
metadata:
  name: acme-routes
  annotations:
    kolihub.io/acme.local: primary
spec:
  rules:
  - host: acme.local
    http:
      paths:
      - path: /
        backend:
          serviceName: web
          servicePort: 80
  - host: duck.acme.local
    http:
      paths:
      - path: /
        backend:
          serviceName: web
          servicePort: 80
  - host: marvin.acme.local
    http:
      paths:
      - path: /web
        backend:
          serviceName: web
          servicePort: 80
      - path: /hello
        backend:
          serviceName: hello
          servicePort: 80
EOF
```

5) **Expose Kong Proxy and access the services**

```bash
kubectl -n kong-system patch service kong-proxy -p '{"spec": {"externalIPs": ["'$(minikube ip)'"]}}'
```

Assuming the domains are mapped in `/etc/hosts` file, it's possible to access the services through Kong at:

- `http://acme.local:8000`
- `http://duck.acme.local:8000`
- `http://marvin.acme.local:8000/web`
- `http://marvin.acme.local:8000/hello`

You could perform a HTTP request with CURL and use the `Host` header to fake the access to a specific route:

```bash
curl http://$(minikube ip):8000/web -H 'Host: marvin.acme.local'
```

## Known Issues/Limitations

- Removing a namespace from the `delegates` field in a domain resource will not trigger an update to the child resources
- It's possible to register a "subdomain" as primary, thus an user could register a subdomain which he doesn't own, e.g.: coyote.acme.org
- Removing an ingress resource doesn't remove the associated Kong routes

Read more at [docs.](./docs/README.md)
