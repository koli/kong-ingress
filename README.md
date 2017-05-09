# Kong Ingress

[![Docker Repository on Quay](https://quay.io/repository/koli/kong-ingress/status "Docker Repository on Quay")](https://quay.io/repository/koli/kong-ingress)

It's a Kubernetes [Ingress](https://kubernetes.io/docs/concepts/services-networking/ingress/) Controller for [Kong](https://getkong.org/about) which manages Kong apis for each existent host on ingresses resources.

## What's is an Ingress Controller

An Ingress Controller is a daemon, deployed as a Kubernetes Pod, that watches the apiserver's /ingresses endpoint for updates to the Ingress resource. Its job is to satisfy requests for ingress.

## Important Note

- This is a work in progress project.
- It relies on a beta Kubernetes resource.

## Overview

Kong it's an API Gateway that deals with L7 traffic, the ingress uses the kong admin API for managing the [apis resources](https://getkong.org/docs/0.10.x/admin-api/#api-object). Each existent host on an ingress spec could map several apis on Kong enabling path based routing. The main object of this controller is to act as an orchestrator of domains and routes on Kong. Load balancing between containers could be achieved using [Services](https://kubernetes.io/docs/concepts/services-networking/service/) to allow external access to Kong it's possible to expose Kong as a [NodePort Service](https://kubernetes.io/docs/concepts/services-networking/service/#type-nodeport) and route traffic to that port.

### Domain Claims

Some of the main problems of using name based virtual hosting with ingress is that you can't know who's is the owner of a specific host, thus a Kong api could be updated by multiple ingress resources resulting in a unwanted behaviour.

A third party resource is used to allow the kong ingress to lease domains for each host specified in ingress resources. If a domain is already claimed in the cluster, the controller rejects the creation of apis on Kong.

[More info](https://raw.githubusercontent.com/kolihub/kong-ingress/master/docs/domain-claims.md)

> More info about the issue: https://github.com/kubernetes/kubernetes/issues/30151

## Prerequisites

- Kubernetes cluster v1.6.0+
- Kubernetes DNS add-on
- Kong server v0.10.0+

## Quick Start

Install [minikube](https://github.com/kubernetes/minikube) because is the quickest way to get a local Kubernetes.

```bash
export CLUSTERDNS=$(kubectl get svc kube-dns -n kube-system --template {{.spec.clusterIP}})

# Install a Kong Server
kubectl create -f https://raw.githubusercontent.com/kolihub/kong-ingress/master/docs/examples/kong-server.yaml
kubectl patch deployment -n kong-system kong -p \
  '{"spec": {"template": {"spec":{"containers":[{"name": "kong", "env":[{"name": "KONG_DNS_RESOLVER", "value": '\"$CLUSTERDNS\"'}]}]}}}}'

# Install the Kong Ingress Controller
kubectl create -f https://raw.githubusercontent.com/kolihub/kong-ingress/master/docs/examples/install.yaml

# Expose Kong
kubectl expose deployment kong -n kong-system --name kong-proxy --external-ip=$(minikube ip) --port 8000 --target-port 8000

# Wait for all pods to be ready
kubectl get pod -n kong-system -w
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

Assuming the domains are mapped in `/etc/hosts` file, it's possible to access the services through Kong at:

- `http://acme.local:8000`
- `http://duck.acme.local:8000`
- `http://marvin.acme.local:8000/web`
- `http://marvin.acme.local:8000/hello`

## Known Issues/Limitations

- Removing a namespace from the `delegates` field in a domain resource will not trigger an update to the child resources
- It's possible to register a "subdomain" as primary, thus an user could register a subdomain which he doesn't own, e.g.: coyote.acme.org
- Removing an ingress resource doesn't remove the associated Kong routes

Read more at [docs.](./docs/README.md)
