#!/bin/bash
set -x -o pipefail

CATALOGSOURCE="test-multipath-operator"
NS="multipath-system"
OPERATOR="multipath-operator"
VERSION="${VERSION:-6.6.6}"
REGISTRY="${REGISTRY:-kuemper.int.rhx/bandini}"

oc delete multipath/multipath-sample
oc delete deployments --all -n "${NS}"
oc delete pods --all -n "${NS}"
oc delete csvs -n ${NS} multipath-operator.v${VERSION}
oc delete -n ${NS} subscription/${OPERATOR}
oc delete -n openshift-marketplace catalogsource/${CATALOGSOURCE}
oc delete ns/${NS}

for i in $(oc get nodes -l node-role.kubernetes.io/worker -o name); do
   echo "$i"
   oc debug -q $i -- chroot /host sh -c "crictl images |grep mul|awk '{ print \$3 }'|xargs -r crictl rmi "
done
