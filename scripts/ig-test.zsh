#!/usr/bin/env zsh

script_dir=${0:A:h}
#source ${script_dir}/_pre.zsh

#
# Prep env
#

KIND_VERSION=v0.33.0

# return error if kubectl is not available
if [[ =kubectl == '' ]]; then
  echo "kubectl not installed"
  return 1
fi

# we use kind to establish a local testing cluster
if [[ =kind == '' ]]; then
  go install sigs.k8s.io/kind@${KIND_VERSION}
fi

#
# Establish a local testing cluster
#

# remove stale kubetap clusters if they exist
_kubetap_kind_clusters=$(kind get clusters 2>&1)
if [[ ${_kubetap_kind_clusters} == *'kubetap'* ]]; then
  kind delete cluster --name kubetap
fi
unset _kubetap_kind_clusters

# catch sigints and exits to delete the cluster, keeping the last exit code
trap '{ e=${?}; sleep 1; kind delete cluster --name kubetap ; exit ${e} }' SIGINT SIGTERM EXIT
kind create cluster --name kubetap

#
# Test kubetap against simple web server deployments
#
_kubetap_test_images=(
  'nginx:1.31.7-alpine@sha256:892bb2152464f0c5d8972593a5f5afaf8054552580b699d95f8ff4af08c2d33d'
  'httpd:2.4.69-alpine@sha256:3440c39d8d6f54fa9ad2549e5a60c19ddd435faadc29c1ad28aa795f71888889'
)
_kubetap_test_services=('nginx' 'httpd')
_kubetap_test_svc_port=('80' '80')

typeset -i _kubetap_iter
for image in ${_kubetap_test_images[@]}; do
  ((_kubetap_iter+=1))
  _kubetap_port=${_kubetap_test_svc_port[${_kubetap_iter}]}
  _kubetap_service=${_kubetap_test_services[${_kubetap_iter}]}

  kubectl --context kind-kubetap create deployment ${_kubetap_service} --image=${image} --port=${_kubetap_port}
  kubectl --context kind-kubetap expose deployment ${_kubetap_service} --port=${_kubetap_port}
  kubectl --context kind-kubetap rollout status deployment/${_kubetap_service} --timeout=120s
  kubectl tap on ${_kubetap_service} -p${_kubetap_port} --context kind-kubetap
  sleep 20

  _kubetap_ready_state=""
  for i in {0..20}; do
    sleep 6
    _kubetap_pod=($(kubectl --context kind-kubetap get pods -ojsonpath='{.items[*].metadata.name}'))
    if (( ${#_kubetap_pod} != 1 )); then
      continue
    fi
    _kubetap_ready_state=$(kubectl --context kind-kubetap get pod ${_kubetap_pod} -ojsonpath='{.status.containerStatuses[*].ready}')
    if [[ ${_kubetap_ready_state} == 'true true' ]]; then
      break
    fi
  done
  if [[ ${_kubetap_ready_state} != 'true true' ]]; then
    echo "container did not come up within 90 seconds"
    return 1
  fi
  unset _kubetap_pod _kubetap_ready_state i

  sleep 1
  kubectl port-forward svc/${_kubetap_service} -n default 2244:2244 &
  _kubetap_pf_one_pid=${!}
  kubectl port-forward svc/${_kubetap_service} -n default 4000:${_kubetap_port} &
  _kubetap_pf_two_pid=${!}
  sleep 5

  # check that we can reach both services
  curl -v http://127.0.0.1:2244 || return 1
  curl -v http://127.0.0.1:4000 || return 1
  # TODO: should also check the mitmproxy JSON resp body to check that it's connected
  kill ${_kubetap_pf_one_pid}
  kill ${_kubetap_pf_two_pid}
  unset _kubetap_pf_one_pid _kubetap_pf_two_pid

  # cleanup test
  kubectl tap off ${_kubetap_service} --context kind-kubetap
  kubectl --context kind-kubetap delete service,deployment ${_kubetap_service} --wait

  unset _kubetap_port _kubetap_service
done
unset _kubetap_test_images _kubetap_test_services _kubetap_test_svc_port _kubetap_iter

#source ${script_dir}/_post.zsh
