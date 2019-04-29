# Proxmox VE Docker Machine Driver - BETA

The incomplete state is over, as I have a working configuration:

* [Download](https://github.com/mhermosi/docker-machine-driver-proxmoxve/releases) or build your own driver
* Copy to some location that is in your path
* Check if it works:

        $ docker-machine create --driver proxmoxve --help | grep -c proxmoxve
        28

* Create your own `boot2docker` ISO to have the guest agent integrated [boot2docker Pull 1319](https://github.com/boot2docker/boot2docker/pull/1319) ([Direct Download in original forked repo](https://github.com/lnxbil/boot2docker/releases/tag/2018-09-16))
* I am testing this with Rancher OS, so you can download the iso from them.
  Here are the links for the rancher os images I am using: ([rancheros-autoformat.iso] (https://releases.rancher.com/os/v1.5.1/proxmoxve/rancheros-autoformat.iso))
     
* Create a script with the following contents and adapt to your needs:

```sh
PRIVATE_KEY=$(cat <<EOF
-----BEGIN RSA PRIVATE KEY-----
....
-----END RSA PRIVATE KEY-----
EOF
)
PUBLIC_KEY=$(cat <<EOF
ssh-rsa AAAAB3Nz...
EOF
)

AUTH_KEYS=$(cat <<EOF
ssh-rsa AAAAB3N...
ssh-rsa AAAAB3N...
ssh-rsa AAAAB3N...
EOF
)

docker-machine --debug \
  create -d proxmoxve \
    --proxmoxve-host "my_proxmoxve_host.local" \
    --proxmoxve-port "8006" \
    --proxmoxve-node "pve" \
    --proxmoxve-user "root" \
    --proxmoxve-realm "pam" \
    --proxmoxve-password "MyProxmoxUserPass" \
    --proxmoxve-net-model "virtio" \
    --proxmoxve-net-bridge "vmbr0" \
    --proxmoxve-cpu-type "kvm64" \
    --proxmoxve-cpu-numa \
    --proxmoxve-cpu-pcid \
    --proxmoxve-cpu-spec-ctrl \
    --proxmoxve-cpu-sockets 2 \
    --proxmoxve-cpu-cores 2 \
    --proxmoxve-disksize-gb 16 \
    --proxmoxve-image-file "local:iso/rancheros-proxmoxve-autoformat.iso" \
    --proxmoxve-storage "local-lvm" \
    --proxmoxve-storage-type "raw" \
    --proxmoxve-guest-username "rancher" \
    --proxmoxve-memory-gb 8 \
    --proxmoxve-driver-debug \
    --proxmoxve-resty-debug \
    --proxmoxve-guest-ssh-authorized-keys "${AUTH_KEYS}" \
    --proxmoxve-guest-ssh-private-key "${PRIVATE_KEY}" \
    --proxmoxve-guest-ssh-public-key "${PUBLIC_KEY}" \
```
