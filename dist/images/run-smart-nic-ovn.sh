docker run --pid host --network host --user=0 --name ovn -dit --cap-add=SYS_NICE -v /var/run/secrets:/var/run/secrets \
 -v /etc/kubernetes/admin.conf:/etc/kubernetes/admin.conf  -v /var/run/dbus:/var/run/dbus:ro -v \
 /var/log/openvswitch:/var/log/openvswitch -v /var/log/openvswitch:/var/log/ovn -v  \
 /var/run/openvswitch:/var/run/openvswitch -v /var/run/openvswitch:/var/run/ovn -v \
 /etc/ovn:/ovn-cert:ro -e OVN_DAEMONSET_VERSION=3 -e OVN_LOG_CONTROLLER="-vconsole:info" \
 -e K8S_APISERVER=$MASTER_IP:6443 -e OVN_KUBERNETES_NAMESPACE=ovn-kubernetes -e OVN_SSL_ENABLE=no \
 -e KUBECONFIG=$KUBECONFIG -e K8S_NODE=bf-node-worker1 -e OVS_RUNDIR=/var/run/openvswitch \
 -e OVS_LOGDIR=/var/log/openvswitch --entrypoint=/root/ovnkube.sh  ovn-daemonset "ovn-controller"
