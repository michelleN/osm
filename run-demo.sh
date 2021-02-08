#!/bin/bash


make build-osm
make docker-push
bin/osm install --enable-routes-v2-experimental=true --container-registry "$CTR_REGISTRY" --osm-image-tag "$CTR_TAG" --osm-chart-path charts/osm
kubectl create namespace bookbuyer
kubectl create namespace bookstore
kubectl create namespace bookwarehouse
kubectl create namespace bookthief
bin/osm namespace add bookstore bookbuyer bookthief bookwarehouse
kubectl apply -f experimental/routes_refactor/demo/manifests/