#!/usr/bin/env bats

@test "ExternalTrafficPolicy: Local" {
    kubectl apply -f "$BATS_TEST_DIRNAME"/../examples/loadbalancer_etp_local.yaml
    kubectl wait --for=condition=ready pods -l app=MyLocalApp
    for i in {1..5}
    do
        IP=$(kubectl get services lb-service-local --output jsonpath='{.status.loadBalancer.ingress[0].ip}')
        [[ ! -z "$IP" ]] && break || sleep 1
    done
    echo $IP
    POD=$(kubectl get pod -l app=MyLocalApp -o jsonpath='{.items[0].metadata.name}')
    echo $POD
    for i in {1..5}
    do
        HOSTNAME=$(curl -s http://${IP}:80/hostname || true)
        [[ ! -z "$HOSTNAME" ]] && break || sleep 1
    done
    echo $HOSTNAME
    [  "$HOSTNAME" = "$POD" ]
}

@test "ExternalTrafficPolicy: Cluster" {
    kubectl apply -f "$BATS_TEST_DIRNAME"/../examples/loadbalancer_etp_cluster.yaml
    kubectl wait --for=condition=ready pods -l app=MyClusterApp
    for i in {1..5}
    do
        IP=$(kubectl get services lb-service-cluster --output jsonpath='{.status.loadBalancer.ingress[0].ip}')
        [[ ! -z "$IP" ]] && break || sleep 1
    done
    echo $IP
    POD=$(kubectl get pod -l app=MyClusterApp -o jsonpath='{.items[0].metadata.name}')
    echo $POD
    for i in {1..5}
    do
        HOSTNAME=$(curl -s http://${IP}:80/hostname || true)
        [[ ! -z "$HOSTNAME" ]] && break || sleep 1
    done
    echo $HOSTNAME
    [  "$HOSTNAME" = "$POD" ]
}

@test "Multiple Protocols: UDP and TCP" {
    kubectl apply -f "$BATS_TEST_DIRNAME"/../examples/loadbalancer_etp_cluster.yaml
    kubectl wait --for=condition=ready pods -l app=MyClusterApp
    for i in {1..5}
    do
        IP=$(kubectl get services lb-service-cluster --output jsonpath='{.status.loadBalancer.ingress[0].ip}')
        [[ ! -z "$IP" ]] && break || sleep 1
    done
    echo $IP
    POD=$(kubectl get pod -l app=MyClusterApp -o jsonpath='{.items[0].metadata.name}')
    echo $POD
    for i in {1..5}
    do
        HOSTNAME=$(curl -s http://${IP}:80/hostname || true)
        [[ ! -z "$HOSTNAME" ]] && break || sleep 1
    done
    echo $HOSTNAME
    [  "$HOSTNAME" = "$POD" ]

        for i in {1..5}
    do
        HOSTNAME=$(echo hostname | nc -u ${IP} 80 || true)
        [[ ! -z "$HOSTNAME" ]] && break || sleep 1
    done
    echo $HOSTNAME
    [  "$HOSTNAME" = "$POD" ]
}