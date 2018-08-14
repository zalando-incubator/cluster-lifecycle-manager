# Cluster Lifecycle Manager (CLM)

[![Build Status](https://travis-ci.org/zalando-incubator/cluster-lifecycle-manager.svg?branch=master)](https://travis-ci.org/zalando-incubator/cluster-lifecycle-manager)
[![Coverage Status](https://coveralls.io/repos/github/zalando-incubator/cluster-lifecycle-manager/badge.svg?branch=master)](https://coveralls.io/github/zalando-incubator/cluster-lifecycle-manager?branch=master)
[![Go Report Card](https://goreportcard.com/badge/github.com/zalando-incubator/cluster-lifecycle-manager)](https://goreportcard.com/report/github.com/zalando-incubator/cluster-lifecycle-manager)

The Cluster Lifecycle Manager (CLM) is a component responsible for operating
(create, update, delete) Kubernetes clusters. It interacts with a Cluster
Registry and a configuration source from which it reads information about the
clusters and keep them up to date with the latest configuration.

![clm](docs/images/cluster-lifecycle-manager.svg)

The CLM is designed to run either as a CLI tool for launching clusters directly
from your development machine, or as a controller running as a single instance
operating many clusters.

It is designed in a reentrant way meaning it can be killed at any point in time
and it will just continue any cluster updates from where it left off. All state
is stored in the Cluster Registry and the git configuration repository.

For a better understanding on how we use the CLM within Zalando, see the 2018 KubeCon EU talk:
* [Continuously Deliver your Kubernetes Infrastructure - Mikkel Larsen, Zalando SE](https://www.youtube.com/watch?v=1xHmCrd8Qn8).

## Current state

The CLM has been developed internally at Zalando since January 2017. It's
currently used to operate 80+ clusters on AWS where the oldest clusters has
been continuously updated all the way from Kubernetes v1.4 to Kubernetes v1.9
by the CLM.

It is currently tightly coupled with our [production cluster
configuration](https://github.com/zalando-incubator/kubernetes-on-aws), but by
making it Open Source and developing it in the open going forward we aim to
make the CLM useful as a generic solution for operating Kubernetes clusters at
scale.

### Features

* Automatically trigger cluster updates based on changes to a Cluster Registry
  defined either as an HTTP REST API or a yaml file.
* Automatically trigger cluster updates based on configuration changes, where
  configuration is stored in a remote git repository or a local directory.
* Perform [Non-disruptive Rolling Updates](#non-disruptive-rolling-updates) of
  nodes in a cluster especially with respect to stateful applications.
* Declarative [deletion](#deletions) of decommissioned cluster resources.

## How to build it

This project uses [Go modules](https://github.com/golang/go/wiki/Modules) as
introduced in Go 1.11 therefore you need Go >=1.11 installed in order to build.
If using Go 1.11 you also need to [activate Module
support](https://github.com/golang/go/wiki/Modules#installing-and-activating-module-support).

Assuming Go has been setup with module support it can be built simply by running:

```sh
export GO111MODULE=on # needed if the project is checked out in your $GOPATH.
$ make
```

## How to run it

To run CLM you need to provide at least the following information:

* URI to a registry `--registry` either a file path or a url to a cluster
  registry.
* A `$TOKEN` used for authenticating with the target Kubernetes cluster once it
  has been provisioned (the `$TOKEN` is an assumption of the Zalando setup, we
  should support a generic `kubeconfig` in the future).
* URL to repository containing the configuration `--git-repository-url` or, in
  alternative, a directory `--directory`

### Run CLM locally

To run CLM locally you can use the following command. This assumes valid AWS
credentials on your machine e.g. in `~/.aws/credentials`.

```sh
$ ./build/clm provision \
  --registry=clusters.yaml \
  --token=$TOKEN \
  --directory=/path/to/configuration-folder \
  --debug
```

The `provision` command does a cluster *create* or *update* depending on
whether the cluster already exists. The other command is `decommission` which
terminates the cluster.

The `clusters.yaml` is of the following format:

```yaml
clusters:
- id: cluster-id
  alias: alias-for-cluster-id # human readable alias
  local_id: local-cluster-id  # used for separating clusters in the same AWS account
  api_server_url: https://kube-api.example.org
  config_items:
    custom_config_item: value # custom key/value config items
  criticality_level: 1
  environment: test
  infrastructure_account: "aws:12345678910" # AWS account ID
  region: eu-central-1
  provider: zalando-aws
  node_pools:
  - name: master-default
    profile: master-default
    min_size: 2
    max_size: 2
    instance_type: m5.large
    discount_strategy: none
  - name: worker-default
    profile: worker-default
    min_size: 3
    max_size: 20
    instance_type: m5.large
    discount_strategy: none
```

## Deletions

By default the Cluster Lifecycle Manager will just apply any manifest defined
in the manifests folder. In order to support deletion of deprecated resources
the CLM will read a `deletions.yaml` file of the following format:

```yaml
pre_apply: # everything defined under here will be deleted before applying the manifests
- name: mate
  namespace: kube-system
  kind: deployment
post_apply: # everything defined under here will be deleted after applying the manifests
- namespace: kube-system
  kind: deployment
  labels:
    application: external-dns
    version: "v1.0"
```

Whatever is defined in this file will be deleted pre/post applying the other
manifest files, if the resource exists. If the resource has already been
deleted previously it's treated as a no-op.

A resource can be identified either by `name` or `labels` if both are defined
the `name` will be used. If none of them are defined, it's an error.

`namespace` can be left out, in which case it will default to `kube-system`.

`kind` must be one of the kinds defined in `kubectl get`.

## Configuration defaults

CLM will look for a `config-defaults.yaml` file in the cluster configuration
directory. If the file exists, it will be evaluated as a Go template with all
the usual CLM variables and functions available, and the resulting output will
be parsed as a simple key-value map. CLM will use the contents of the file to
populate the cluster's configuration items, taking care not to overwrite the
existing ones.

For example, you can use the defaults file to have different settings for
production and test clusters, while keeping the manifests readable:

* **config-defaults.yaml**:
    ```yaml
    {{ if eq .Environment "production"}}
    autoscaling_buffer_pods: "3"
    {{else}}
    autoscaling_buffer_pods: "0"
    {{end}}
    ```

* **manifests/example/example.yaml**:
    ```yaml
    …
    spec:
      replicas: {{.ConfigItems.autoscaling_buffer_pods}}
    …
    ```

## Non-disruptive rolling updates

One of the main features of the CLM is the update strategy implemented which is
designed to do rolling node updates which are non-disruptive for workloads
running in the target cluster. Special care is taken to support stateful
applications.
