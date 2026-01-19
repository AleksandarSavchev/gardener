#!/usr/bin/env bash

namespace=${1:-shoot--local--local}

pods_list=$(kubectl get pods -n "${namespace}" -o yaml)

apiserver_pods=$(echo "${pods_list}" | yq '.items[] | select(.metadata.labels.role == "apiserver")')

encr_secret=$(echo "${apiserver_pods}" | yq '.spec.volumes[] | select(.name == "etcd-encryption-secret") | .secret.secretName')
echo "Encryption secret: ${encr_secret}"

secret=$(kubectl get secret -n "${namespace}" "${encr_secret}" -o yaml)

encr_config=$(echo "${secret}" | yq '.data."encryption-configuration.yaml"' | base64 --decode)

echo "${encr_config}"

## Why does rotate annotation work for encryption config: https://github.com/gardener/gardener/blob/0c3219b3f31a6dbdf760e47e392a99a91221d2df/pkg/utils/secrets/manager/generate.go#L50