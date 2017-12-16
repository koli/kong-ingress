# Kong Ingress

[![Build Status](https://travis-ci.org/koli/kong-ingress.svg?branch=master)](https://travis-ci.org/koli/kong-ingress)

It's a Kubernetes [Ingress](https://kubernetes.io/docs/concepts/services-networking/ingress/) Controller for [Kong](https://getkong.org/about) which manages Kong apis for each existent host on ingresses resources.

## What's is an Ingress Controller

An Ingress Controller is a daemon, deployed as a Kubernetes Pod, that watches the apiserver's /ingresses endpoint for updates to the Ingress resource. Its job is to satisfy requests for ingress.

## Important Note

- This is a work in progress project.
- It relies on a beta Kubernetes resource.

## Overview

Kong it's an API Gateway that deals with L7 traffic, the ingress uses the kong admin API for managing the [apis resources](https://getkong.org/docs/0.10.x/admin-api/#api-object). Each existent host on an ingress spec could map several apis on Kong enabling path based routing. The main object of this controller is to act as an orchestrator of domains and routes on Kong. Load balancing between containers could be achieved using [Services](https://kubernetes.io/docs/concepts/services-networking/service/) to allow external access to Kong it's possible to expose Kong as a [NodePort Service](https://kubernetes.io/docs/concepts/services-networking/service/#type-nodeport) and route traffic to that port.

### Domain Claims

Some of the main problems of using name based virtual hosting with ingress is that you can't know who's the owner of a specific host, thus a Kong api could be updated by multiple ingress resources resulting in an unwanted behaviour.

A [Custom Resource Definition](https://kubernetes.io/docs/concepts/api-extension/custom-resources/) is used to allow the kong ingress to lease domains for each host specified in ingress resources. If a domain is already claimed in the cluster, the controller rejects the creation of apis on Kong.

[More info](./docs/domain-claims.md)

> More info about the issue: https://github.com/kubernetes/kubernetes/issues/30151

## Prerequisites

- Kubernetes cluster v1.7.0+
- Kubernetes DNS add-on
- Kong server v0.10.0+

## Quick Start

- Install [minikube](https://github.com/kubernetes/minikube) because is the quickest way to get a local Kubernetes.

Create a namespace for kong and the ingress controller

```bash
kubectl create ns kong-system
```

- Install [Kong on Kubernetes](https://getkong.org/install/kubernetes/) following the minikube instructions.

> Create all the resources in the `kong-system` namespace. E.g.: `kubectl create -f postgres.yaml -n kong-system`.

- Install the Kong Ingress Controller

```bash
KONG_INGRESS_VERSION=v0.3.1-alpha kubectl create -f - <<EOF
apiVersion: extensions/v1beta1
kind: Deployment
metadata:
  name: kong-ingress
  namespace: kong-system
spec:
  replicas: 1
  template:
    metadata:
      labels:
        app: kong-ingress
    spec:
      terminationGracePeriodSeconds: 60
      containers:
      - name: kong-ingress
        image: 'quay.io/koli/kong-ingress:$KONG_INGRESS_VERSION'
        args:
        - --auto-claim
        - --wipe-on-delete
        - --kong-server=http://kong-admin:8001
        - --v=4
        - --logtostderr
        - --tls-insecure
EOF
```

After all pods are in the `Running` state, begin to create your routes. The example above creates two distinct deployments and expose then using services as 'web' and 'hello':

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

The ingress resource below will create 4 routes at Kong, one route for path:

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

Expose Kong Proxy

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
