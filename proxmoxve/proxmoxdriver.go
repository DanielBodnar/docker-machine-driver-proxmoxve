package proxmoxve

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"gopkg.in/resty.v1"
	"io/ioutil"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	sshrw "github.com/mosolovsa/go_cat_sshfilerw"
	"golang.org/x/crypto/ssh"

	"github.com/docker/machine/libmachine/drivers"
	"github.com/docker/machine/libmachine/mcnflag"
	"github.com/docker/machine/libmachine/state"
	"github.com/labstack/gommon/log"
)



// PVE Default values for connection and authentication
const (
	pveDriverName                   = "proxmoxve"
	pveDefaultPort                  = 8006
	pveDefaultUsername              = "root"
	pveDefaultRealm                 = "pam"

	// PVE Default values for PVE resource constants
	pveDefaultStorageLocation       = "local-lvm"
	pveDefaultStorageType           = "raw"

	// PVE VM Default values constants
	pveDefaultVmAgent               = "1"
	pveDefaultVmAutoStart           = "1"
	pveDefaultVmOnBoot              = "1"
	pveDefaultVmOsType              = "l26"
	pveDefaultVmKvm                 = "1"

	pveDefaultVmGuestUserName       = "docker"
	pveDefaultVmGuestUserPassword   = "tcuser"

	pveDefaultVmRootDiskSizeGb      = "16"
	pveDefaultVmMemorySizeGb        = 8

	pveDefaultVmNetBridge           = "vmbr0"
	pveDefaultVmNetModel            = "virtio"

	pveDefaultVmCpuSocketCount      = "1"
	pveDefaultVmCpuCoreCount        = "4"
	pveDefaultVmCpuType             = "kvm64"

	pveDiverMissingOptionMessageFmt = "proxmoxve driver requires the --%s option"
)

// Command Parameters strings
const (
	pveHostParameter                   = "proxmoxve-host"
	pvePortParameter                   = "proxmoxve-port"
	pveUserParameter                   = "proxmoxve-user"
	pveRealmParameter                  = "proxmoxve-realm"
	pvePasswordParameter               = "proxmoxve-password"
	pveNodeParameter                   = "proxmoxve-node"
	pvePoolParameter                   = "proxmoxve-pool"
	pveImageFileParameter              = "proxmoxve-image-file"
	pveStorageParameter                = "proxmoxve-storage"
	pveStorageTypeParameter            = "proxmoxve-storage-type"
	pveDiskSizeGbParameter             = "proxmoxve-disksize-gb"
	pveMemoryGbParameter               = "proxmoxve-memory-gb"
	pveGuestUsernameParameter          = "proxmoxve-guest-username"
	pveGuestPasswordParameter          = "proxmoxve-guest-password"

	pveNetBridgeParameter              = "proxmoxve-net-bridge"
	pveNetModelParameter               = "proxmoxve-net-model"
	pveNetVlanTagParameter             = "proxmoxve-net-vlantag"
	pveCpuSocketsParameter             = "proxmoxve-cpu-sockets"
	pveCpuCoresParameter               = "proxmoxve-cpu-cores"
	pveCpuTypeParameter                = "proxmoxve-cpu-type"
	pveCpuNumaParamater                = "proxmoxve-cpu-numa"


	pveCpuPcidParameter                = "proxmoxve-cpu-pcid"
	pveCpuSpecCtlrParameter            = "proxmoxve-cpu-spec-ctrl"

	pveGuestSshPrivateKeyParameter     = "proxmoxve-guest-ssh-private-key"
	pveGuestSshPublicKeyParameter      = "proxmoxve-guest-ssh-public-key"
	pveGuestSshAuthorizedKeysParameter = "proxmoxve-guest-ssh-authorized-keys"

	pveDriverDebugParameter            = "proxmoxve-driver-debug"
	pveRestyDebugParameter             = "proxmoxve-resty-debug"

	pveSwarmHostParameter              = "swarm-host"
	pveSwarmMastertParameter           = "swarm-master"

)

// Driver for Proxmox VE
type Driver struct {
	*drivers.BaseDriver
	driver                 *ProxmoxVE

	// Basic Authentication for Proxmox VE
	Host                   string // Proxmox VE Server Host name
	Port                   int    // Proxmox VE Server listening port
	Node                   string // optional, node to create VM on, host used if omitted but must match internal node name
	User                   string // username
	Password               string // password
	Realm                  string // realm, e.g. pam, pve, etc.

	// File to load as boot image RancherOS/Boot2Docker
	ImageFile              string // in the format <storagename>:iso/<filename>.iso

	Pool                   string // pool to add the VM to (necessary for users with only pool permission)
	Storage                string // internal PVE storage name
	StorageType            string // Type of the storage (currently QCOW2 and RAW)
	DiskSize               string // disk size in GB
	Memory                 int    // memory in GB
	StorageFilename        string

	VMID                   string // VM ID only filled by create()
	GuestUsername          string // username to log into the guest OS
	GuestPassword          string // password to log into the guest OS to copy the public key

	driverDebug            bool   // driver debugging
	restyDebug             bool   // enable resty debugging

	NetBridge              string // Net was defaulted to vmbr0, but should accept any other config i.e vmbr1
	NetModel               string // Net Interface Model, [e1000, virtio, realtek, etc...]
	NetVlanTag             int // VLAN
	Cores                  string // # of cores on each cpu socket
	Sockets                string // # of cpu sockets

	CpuType                string
	Numa                   bool
	Pcid                   bool
	SpecCtrl               bool

	GuestSSHPrivateKey     string
	GuestSSHPublicKey      string
	GuestSSHAuthorizedKeys string

}

func (d *Driver) debugf(format string, v ...interface{}) {
	if d.driverDebug {
		log.Infof(fmt.Sprintf(format, v...))
	}
}

func (d *Driver) debug(v ...interface{}) {
	if d.driverDebug {
		log.Info(v...)
	}
}

func (d *Driver) connectAPI() error {
	if d.driver == nil {
		d.debugf("Create called")

		d.debugf("Connecting to %s as %s@%s with password '%s'", d.Host, d.User, d.Realm, d.Password)
		c, err := GetProxmoxVEConnectionByValues(d.User, d.Password, d.Realm, d.Host)
		d.driver = c
		if err != nil {
			return fmt.Errorf("Could not connect to host '%s' with '%s@%s'", d.Host, d.User, d.Realm)
		}
		if d.restyDebug {
			c.EnableDebugging()
		}
		d.debugf("Connected to PVE version '" + d.driver.Version + "'")
	}
	return nil
}

func (d *Driver) GetCreateFlags() []mcnflag.Flag {
	return []mcnflag.Flag{
		mcnflag.StringFlag{
			EnvVar: "PROXMOXVE_HOST",
			Name:   pveHostParameter,
			Usage:  "Server Hostname or IP Address",
			Value:  "",
		},
		mcnflag.IntFlag{
			EnvVar: "PROXMOXVE_PORT",
			Name:   pvePortParameter,
			Usage:  "Server port",
			Value:  pveDefaultPort,
		},
		mcnflag.StringFlag{
			EnvVar: "PROXMOXVE_NODE",
			Name:   pveNodeParameter,
			Usage:  "Node name",
			Value:  "",
		},
		mcnflag.StringFlag{
			EnvVar: "PROXMOXVE_USER",
			Name:   pveUserParameter,
			Usage:  "Username",
			Value:  pveDefaultUsername,
		},
		mcnflag.StringFlag{
			EnvVar: "PROXMOXVE_REALM",
			Name:   pveRealmParameter,
			Usage:  "Authentication Realm (default: pam)",
			Value:  pveDefaultRealm,
		},
		mcnflag.StringFlag{
			EnvVar: "PROXMOXVE_PASSWORD",
			Name:   pvePasswordParameter,
			Usage:  "User Password",
			Value:  "",
		},
		mcnflag.StringFlag{
			EnvVar: "PROXMOXVE_STORAGE",
			Name:   pveStorageParameter,
			Usage:  "Storage location for volume creation",
			Value:  pveDefaultStorageLocation,
		},
		mcnflag.StringFlag{
			EnvVar: "PROXMOXVE_STORAGE_TYPE",
			Name:   pveStorageTypeParameter,
			Usage:  "Storage type (QCOW2 or RAW)",
			Value:  pveDefaultStorageType,
		},
		mcnflag.StringFlag{
			EnvVar: "PROXMOXVE_IMAGE_FILE",
			Name:   pveImageFileParameter,
			Usage:  "Storage location of the image file (e.g. local:iso/boot2docker.iso)",
			Value:  "",
		},
		mcnflag.StringFlag{
			EnvVar: "PROXMOXVE_POOL",
			Name:   pvePoolParameter,
			Usage:  "Pool to attach VM",
			Value:  "",
		},
		mcnflag.StringFlag{
			EnvVar: "PROXMOXVE_DISKSIZE_GB",
			Name:   pveDiskSizeGbParameter,
			Usage:  "Root Disk size in GB",
			Value:  pveDefaultVmRootDiskSizeGb,
		},
		mcnflag.IntFlag{
			EnvVar: "PROXMOXVE_MEMORY_GB",
			Name:   pveMemoryGbParameter,
			Usage:  "RAM Memory in GB",
			Value:  pveDefaultVmMemorySizeGb,
		},
		mcnflag.StringFlag{
			Name:   pveGuestUsernameParameter,
			Usage:  "Guest OS account Username (default docker for boot2docker)",
			Value:  pveDefaultVmGuestUserName,
		},
		mcnflag.StringFlag{
			Name:   pveGuestPasswordParameter,
			Usage:  "Guest OS account Password (default tcuser for boot2docker)",
			Value:  pveDefaultVmGuestUserPassword,
		},
		mcnflag.BoolFlag{
			Name:  pveRestyDebugParameter,
			Usage: "Enables the resty debugging",
		},
		mcnflag.BoolFlag{
			Name:  pveDriverDebugParameter,
			Usage: "Enables debugging in the driver",
		},
		mcnflag.StringFlag{
			EnvVar: "PROXMOXVE_NET_BRIDGE",
			Name:   pveNetBridgeParameter,
			Usage:  "Network Bridge (default vmbr0)",
			Value:  pveDefaultVmNetBridge,
		},
		mcnflag.StringFlag{
			EnvVar: "PROXMOXVE_NET_MODEL",
			Name:   pveNetModelParameter,
			Usage:  "Network Interface model (default virtio)",
			Value:  pveDefaultVmNetModel,
		},
		mcnflag.IntFlag{
			EnvVar: "PROXMOXVE_NET_VLANTAG",
			Name:   pveNetVlanTagParameter,
			Usage:  "Network VLan Tag (1 - 4094)",
		},
		mcnflag.StringFlag{
			EnvVar: "PROXMOXVE_CPU_SOCKETS",
			Name:   pveCpuSocketsParameter,
			Usage:  "Number of CPU Sockets (1 - 4)",
			Value:  pveDefaultVmCpuSocketCount,
		},
		mcnflag.StringFlag{
			EnvVar: "PROXMOXVE_CPU_CORES",
			Name:   pveCpuCoresParameter,
			Usage:  "Number of Cores per Socket (1 - 128)",
			Value:  pveDefaultVmCpuCoreCount,
		},
		mcnflag.StringFlag{
			EnvVar: "PROXMOXVE_CPU_TYPE",
			Name:   pveCpuTypeParameter,
			Usage:  "CPU Type (kvm32, kvm64, host, etc)",
			Value:  pveDefaultVmCpuType,
		},
		mcnflag.BoolFlag{
			EnvVar: "PROXMOXVE_CPU_NUMA",
			Name:   pveCpuNumaParamater,
			Usage:  "Enable CPU Numa option",
		},
		mcnflag.BoolFlag{
			EnvVar: "PROXMOXVE_CPU_PCID",
			Name:   pveCpuPcidParameter,
			Usage:  "Enable CPU pcid option",
		},
		mcnflag.BoolFlag{
			EnvVar: "PROXMOXVE_CPU_SPEC_CTRL",
			Name:   pveCpuSpecCtlrParameter,
			Usage:  "Enable cpu spec-ctrl option",
		},
		mcnflag.StringFlag{
			EnvVar: "PROXMOXVE_GUEST_SSH_PRIVATE_KEY",
			Name:   pveGuestSshPrivateKeyParameter,
			Usage:  "SSH Private Key on Guest OS",
			Value:  "",
		},
		mcnflag.StringFlag{
			EnvVar: "PROXMOXVE_GUEST_SSH_PUBLIC_KEY",
			Name:   pveGuestSshPublicKeyParameter,
			Usage:  "SSH Public Key on Guest OS",
			Value:  "",
		},
		mcnflag.StringFlag{
			EnvVar: "PROXMOXVE_GUEST_SSH_AUTHORIZED_KEYS",
			Name:   pveGuestSshAuthorizedKeysParameter,
			Usage:  "SSH Authorized Keys on Guest OS",
			Value:  "",
		},
	}
}

func (d *Driver) ping() bool {
	if d.driver == nil {
		return false
	}

	command := NodesNodeQemuVMIDAgentPostParameter{Command: "ping"}
	err := d.driver.NodesNodeQemuVMIDAgentPost(d.Node, d.VMID, &command)

	if err != nil {
		d.debug(err)
		return false
	}

	return true
}

// DriverName returns the name of the driver
func (d *Driver) DriverName() string {
	return pveDriverName
}

func (d *Driver) SetConfigFromFlags(flags drivers.DriverOptions) error {
	d.debug("SetConfigFromFlags called")

	// Required Parameters:
	d.Host                   = flags.String(pveHostParameter)
	d.Node                   = flags.String(pveNodeParameter)
	d.Password               = flags.String(pvePasswordParameter)
	d.ImageFile              = flags.String(pveImageFileParameter)

	// Required Parameters with default value
	d.Port                   = flags.Int(pvePortParameter)
	d.User                   = flags.String(pveUserParameter)
	d.Realm                  = flags.String(pveRealmParameter)
	d.Storage                = flags.String(pveStorageParameter)
	d.StorageType            = strings.ToLower(flags.String(pveStorageTypeParameter))
	d.DiskSize               = flags.String(pveDiskSizeGbParameter)
	d.Memory                 = flags.Int(pveMemoryGbParameter)
	d.GuestUsername          = flags.String(pveGuestUsernameParameter)
	d.Sockets                = flags.String(pveCpuSocketsParameter)
	d.Cores                  = flags.String(pveCpuCoresParameter)
	d.NetBridge              = flags.String(pveNetBridgeParameter)
	d.NetModel               = flags.String(pveNetModelParameter)

	d.CpuType                = flags.String(pveCpuTypeParameter)
	d.Numa                   = flags.Bool(pveCpuNumaParamater)
	d.Pcid                   = flags.Bool(pveCpuPcidParameter)
	d.SpecCtrl               = flags.Bool(pveCpuSpecCtlrParameter)

	// Optional Paramweters:
	d.Pool                   = flags.String(pvePoolParameter)
	d.GuestPassword          = flags.String(pveGuestPasswordParameter)
	d.NetVlanTag             = flags.Int(pveNetVlanTagParameter)
	d.GuestSSHPrivateKey     = flags.String(pveGuestSshPrivateKeyParameter)
	d.GuestSSHPublicKey      = flags.String(pveGuestSshPublicKeyParameter)
	d.GuestSSHAuthorizedKeys = flags.String(pveGuestSshAuthorizedKeysParameter)

	d.driverDebug            = flags.Bool(pveDriverDebugParameter)
	d.restyDebug             = flags.Bool(pveRestyDebugParameter)

	d.SwarmMaster            = flags.Bool(pveSwarmMastertParameter)
	d.SwarmHost              = flags.String(pveSwarmHostParameter)

	// Adjust and other modifications on parameters
	d.SSHUser                = d.GuestUsername
	d.Memory                *= 1024

	d.debugf("Private key:\n%s\n\nPublic Key:\n%s\n\n", d.GuestSSHPrivateKey, d.GuestSSHPublicKey)

	if d.restyDebug {
		d.debug("enabling Resty debugging")
		resty.SetDebug(true)
	}

	if d.GuestUsername != pveDefaultVmGuestUserName && d.GuestPassword == pveDefaultVmGuestUserPassword {
		d.GuestPassword = ""
	}

	// Required parameters validations
	if d.Host == "" {
		return fmt.Errorf(pveDiverMissingOptionMessageFmt, pveHostParameter)
	}

	if d.Node == "" {
		return fmt.Errorf(pveDiverMissingOptionMessageFmt, pveNodeParameter)
	}

	if d.Password == "" {
		return fmt.Errorf(pveDiverMissingOptionMessageFmt, pvePasswordParameter)
	}

	if d.ImageFile == "" {
		return fmt.Errorf(pveDiverMissingOptionMessageFmt, pveImageFileParameter)
	}

	return nil
}

func (d *Driver) GetURL() (string, error) {
	ip, err := d.GetIP()
	if err != nil {
		return "", err
	}
	if ip == "" {
		return "", nil
	}
	return fmt.Sprintf("tcp://%s:2376", ip), nil
}

func (d *Driver) GetMachineName() string {
	return d.MachineName
}

func (d *Driver) GetIP() (string, error) {
	d.connectAPI()
	return d.driver.GetEth0IPv4(d.Node, d.VMID)
}

func (d *Driver) GetSSHHostname() (string, error) {
	return d.GetIP()
}

//func (d *Driver) GetSSHKeyPath() string {
//	return d.GetSSHKeyPath() + ".pub"
//}

func (d *Driver) GetSSHPort() (int, error) {
	if d.SSHPort == 0 {
		d.SSHPort = 22
	}

	return d.SSHPort, nil
}

func (d *Driver) GetSSHUsername() string {
	if d.SSHUser == "" {
		d.SSHUser = pveDefaultVmGuestUserName
	}

	return d.SSHUser
}

func (d *Driver) GetState() (state.State, error) {
	err := d.connectAPI()
	if err != nil {
		return state.Paused, err
	}

	if d.ping() {
		return state.Running, nil
	}
	return state.Paused, nil
}

func (d *Driver) PreCreateCheck() error {

	switch d.StorageType {
	case "raw":
		fallthrough
	case "qcow2":
		break
	default:
		return fmt.Errorf("storage type '%s' is not supported", d.StorageType)
	}

	err := d.connectAPI()
	if err != nil {
		return err
	}

	d.debug("Retrieving next ID")
	id, err := d.driver.ClusterNextIDGet(0)
	if err != nil {
		return err
	}
	d.debugf("Next ID was '%s'", id)
	d.VMID = id

	storageType, err := d.driver.GetStorageType(d.Node, d.Storage)
	if err != nil {
		return err
	}

	filename := "vm-" + d.VMID + "-disk-0"
	switch storageType {
	case "lvmthin":
		fallthrough
	case "zfs":
		fallthrough
	case "ceph":
		if d.StorageType != "raw" {
			return fmt.Errorf("type '%s' on storage '%s' does only support raw", storageType, d.Storage)
		}
	case "dir":
		filename += "." + d.StorageType
	}
	d.StorageFilename = filename

	// create and save a new SSH key pair
	keyfile := d.GetSSHKeyPath()
	keypath := path.Dir(keyfile)
	d.debugf("Generating new key pair at path '%s'", keypath)
	err = os.MkdirAll(keypath, 0755)
	if err != nil {
		return err
	}
	_, _, err = GetKeyPair(keyfile)

	return err
}

func (d *Driver) Create() error {

	cloudinit := fmt.Sprintf("%s:cloudinit", d.Storage)

	volume := NodesNodeStorageStorageContentPostParameter{
		Filename: d.StorageFilename,
		Size:     d.DiskSize + "G",
		VMID:     d.VMID,
	}

	d.debugf("Creating disk volume '%s' with size '%s'", volume.Filename, volume.Size)
	err := d.driver.NodesNodeStorageStorageContentPost(d.Node, d.Storage, &volume)
	if err != nil {
		return err
	}

	storageDrive := fmt.Sprintf("%s:%s,size=%s", d.Storage, volume.Filename, volume.Size)

	net := fmt.Sprintf("%s,bridge=%s", d.NetModel, d.NetBridge)
	if d.NetVlanTag > 0 {
		net = fmt.Sprintf("%s,tag=%d", net, d.NetVlanTag)
	}

	cpuFlags  := ""
	pcid      := ""
	specCtrl  := ""
	separator := ""
	flags     := false

	if d.SpecCtrl && d.Pcid {
		separator = ";"
		flags = true
	}
	if d.Pcid {
		pcid = "+pcid"
		flags = true
	}
	if d.SpecCtrl {
		specCtrl = "+spec-ctrl"
		flags = true
	}

	if flags {
		cpuFlags = fmt.Sprintf(",flags=%s%s%s",pcid, separator, specCtrl)
	}
	cpuDefinition := fmt.Sprintf("%s%s", d.CpuType, cpuFlags)

	numa := 0
	if d.Numa {
		numa = 1
	}

	if d.GuestSSHPublicKey != "" {
		d.GuestSSHAuthorizedKeys = fmt.Sprintf("%s\n%s\n",d.GuestSSHAuthorizedKeys, d.GuestSSHPublicKey)
	}

	npp := NodesNodeQemuPostParameter{
		VMID:      d.VMID,
		Memory:    d.Memory,
		Autostart: pveDefaultVmAutoStart,
		Agent:     pveDefaultVmAgent,
		Net0:      net, // Added to support bridge differnet from vmbr0 (vlan tag should be supported as well)
		Name:      d.BaseDriver.MachineName,
		SCSI0:     storageDrive,
		Onboot:    pveDefaultVmOnBoot,
		Ostype:    pveDefaultVmOsType,
		KVM:       pveDefaultVmKvm, // if you test in a nested environment, you may have to change this to 0 if you do not have nested virtualization
		Pool:      d.Pool,
		Sockets:   d.Sockets,
		Cores:     d.Cores,
		Cdrom:     d.ImageFile,
		SshKeys:   strings.Replace(url.QueryEscape(d.GuestSSHAuthorizedKeys), "+", "%20", -1), // d.GuestSSHAuthorizedKeys,
		CPU:       cpuDefinition,
		Numa:      numa,
		Citype:    "nocloud",
		Ciuser:    d.GuestUsername,
		IDE0:      cloudinit,
	}

	if d.StorageType == "qcow2" {
		npp.SCSI0 = d.Storage + ":" + d.VMID + "/" + volume.Filename
	}
	d.debugf("Creating VM '%s' with '%d' of memory", npp.VMID, npp.Memory)
	err = d.driver.NodesNodeQemuPost(d.Node, &npp)
	if err != nil {
		// make sure rto remove the created volume
		d.debugf("Removing disk volume '%s' with size '%s'", volume.Filename, volume.Size)
		d.driver.NodesNodeStorageStorageContentDelete(d.Node, d.Storage, volume.Filename)
		return err
	}

	d.Start()

	err = d.waitAndPrepareSSH()
	if err != nil {
		return err
	}

	ip, err := d.GetIP()
	if err != nil {
		return err
	}
	d.IPAddress = ip
	return nil
}

func (d *Driver) waitAndPrepareSSH() error {

	sshUser := d.GetSSHUsername()
	d.debugf("waiting for VM to become active, first wait 10 seconds")
	time.Sleep(10 * time.Second)

	for !d.ping() {
		d.debugf("waiting for VM to become active")
		time.Sleep(2 * time.Second)
	}
	d.debugf("VM is active waiting more")
	time.Sleep(2 * time.Second)



	sshConfig := &ssh.ClientConfig{
		User: sshUser,
		Auth: []ssh.AuthMethod{
			ssh.Password(pveDefaultVmGuestUserPassword),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	sshbasedir := "/home/" + sshUser + "/.ssh"
	hostname, _ := d.GetSSHHostname()
	port, _ := d.GetSSHPort()
	clientstr := fmt.Sprintf("%s:%d", hostname, port)

	d.debugf("Creating directory '%s' on client: %s", sshbasedir, clientstr)
	conn, err := ssh.Dial("tcp", clientstr, sshConfig)
	if err != nil {
		return err
	}
	session, err := conn.NewSession()
	if err != nil {
		return err
	}

	var stdoutBuf bytes.Buffer
	session.Stdout = &stdoutBuf
	session.Run("mkdir -p " + sshbasedir)
	d.debugf(fmt.Sprintf("%s -> %s", hostname, stdoutBuf.String()))
	session.Close()

	d.debugf("Trying to copy to %s:%s", clientstr, sshbasedir)
	c, err := sshrw.NewSSHclt(clientstr, sshConfig)
	if err != nil {
		return err
	}

	// Open a file
	f, err := os.Open(d.GetSSHKeyPath() + ".pub")
	if err != nil {
		return err
	}

	// TODO: always fails with return status 127, but file was copied correclty
	c.WriteFile(f, sshbasedir+"/authorized_keys")
	// if err = c.WriteFile(f, sshbasedir+"/authorized_keys"); err != nil {
	// 	d.debugf("Error on file write: ", err)
	// }

	// Close the file after it has been copied
	defer f.Close()

	return err
}

func (d *Driver) Start() error {
	err := d.connectAPI()
	if err != nil {
		return err
	}
	return d.driver.NodesNodeQemuVMIDStatusStartPost(d.Node, d.VMID)
}

func (d *Driver) Stop() error {
	//d.MockState = state.Stopped
	return nil
}

func (d *Driver) Restart() error {
	d.Stop()
	d.Start()
	//d.MockState = state.Running
	return nil
}

func (d *Driver) Kill() error {
	//d.MockState = state.Stopped
	return nil
}

func (d *Driver) Remove() error {
	err := d.connectAPI()
	if err != nil {
		return err
	}
	return d.driver.NodesNodeQemuVMIDDelete(d.Node, d.VMID)
}

func (d *Driver) Upgrade() error {
	return nil
}

func NewDriver(hostName, storePath string) drivers.Driver {
	return &Driver{
		BaseDriver: &drivers.BaseDriver{
			SSHUser:     pveDefaultVmGuestUserName,
			MachineName: hostName,
			StorePath:   storePath,
		},
	}
}

func GetKeyPair(file string) (string, string, error) {
	// read keys from file
	_, err := os.Stat(file)
	if err == nil {
		priv, err := ioutil.ReadFile(file)
		if err != nil {
			fmt.Printf("Failed to read file - %s", err)
			goto genKeys
		}
		pub, err := ioutil.ReadFile(file + ".pub")
		if err != nil {
			fmt.Printf("Failed to read pub file - %s", err)
			goto genKeys
		}
		return string(pub), string(priv), nil
	}

	// generate keys and save to file
genKeys:
	pub, priv, err := GenKeyPair()
	err = ioutil.WriteFile(file, []byte(priv), 0600)
	if err != nil {
		return "", "", fmt.Errorf("Failed to write file - %s", err)
	}
	err = ioutil.WriteFile(file+".pub", []byte(pub), 0644)
	if err != nil {
		return "", "", fmt.Errorf("Failed to write pub file - %s", err)
	}

	return pub, priv, nil
}

func GenKeyPair() (string, string, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", "", err
	}

	privateKeyPEM := &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)}
	var private bytes.Buffer
	if err := pem.Encode(&private, privateKeyPEM); err != nil {
		return "", "", err
	}

	// generate public key
	pub, err := ssh.NewPublicKey(&privateKey.PublicKey)
	if err != nil {
		return "", "", err
	}

	public := ssh.MarshalAuthorizedKey(pub)
	return string(public), private.String(), nil
}
