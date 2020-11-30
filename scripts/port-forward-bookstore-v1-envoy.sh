#!/bin/bash


# This script port forwards from the bookstore-v1 pod to local port 15001


# shellcheck disable=SC1091
source .env

BOOKBUYER_LOCAL_PORT="${BOOKBUYER_LOCAL_PORT:-15000}"
POD="$(kubectl get pods --selector app=bookbuyer -n "$BOOKSTORE_NAMESPACE" --no-headers | grep 'Running' | awk '{print $1}')"

kubectl port-forward "$POD" -n "$BOOKSTORE_NAMESPACE" "$BOOKSTORE_LOCAL_PORT":15001
