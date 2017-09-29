> :warning: Note: This is only for testing pourpose. Make sure you do the proper changes when running in production, Kong has its own YAMLs for that [here](https://github.com/Mashape/kong-dist-kubernetes)

Kong can easily be provisioned to Minikube cluster using the following steps:

1.  **Deploy Kubernetes via Minikube**
    
    You need [minikube](https://kubernetes.io/docs/tasks/tools/install-minikube/) and
    [kubectl](https://kubernetes.io/docs/tasks/tools/install-kubectl/)
    command-line tools installed and set up to run deployment commands.

    Using the `minikube` command, deploy a Kubernetes cluster.

    ```bash
    $ minikube start
    ```

    By now, you have provisioned a Kubernetes managed cluster locally.

2. **Deploy a Kong**

    This process will create the resources:
    - `Namespace`: kong-system
    - `Services`: postgres and kong-admin
    - `ReplicationController`: postgres
    - `Deployment`: kong

    ```bash
    $ kubectl create -f kong-server.yaml
    ```

3. **Prepare database**

    Using the `kong-migration.yaml` file from this repo,
    run the migration job, jump to step 5 if Kong backing databse is up–to–date:
    
    ```bash
    $ kubectl create -f kong-migration.yaml
    ```
    Once job completes, you can remove the pod by running following command:

    ```bash
    $ kubectl delete -f kong-migration.yaml
    ```

4. **Deploy Ingress Controller**
    Now we will deploy the kong ingress controller by running:

    ```bash
    $ kubectl create -f kong-ingress.yaml
    ```

5. **Verify your deployments**

    You can now see the resources that have been deployed using `kubectl`:

    ```bash
    $ kubectl get all
    ```

    Once the Kong services are started, you can test Kong by making the
    following requests:

    ```bash
    $ curl $(minikube service --url kong-admin)
    $ curl $(minikube service --url kong-proxy|head -n1)
    ```

    It may take up to 3 minutes for all services to come up.

7. **Using Kong**

    Quickly learn how to use Kong with the 
    [5-minute Quickstart](https://getkong.org/docs/latest/getting-started/quickstart/).