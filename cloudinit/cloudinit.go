package cloudinit

import (
	"bytes"
	"compress/gzip"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/constantes"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/pkg/apis/nodemanager/v1alpha1"
	glog "github.com/sirupsen/logrus"
	"golang.org/x/crypto/ssh"
	"gopkg.in/yaml.v2"
)

const GzipBase64 = "gzip+base64"

// GuestInfos the guest infos
// Must not start with `guestinfo.`
type GuestInfos map[string]string

type CloudInit map[string]any

type CloudInitInput struct {
	InstanceName string
	InstanceID   string //string(uuid.NewUUID())
	DomainName   string
	UserName     string
	AuthKey      string
	TimeZone     string
	Network      *NetworkDeclare
	AllowUpgrade bool
	CloudInit    CloudInit
}

// NetworkResolv /etc/resolv.conf
type NetworkResolv struct {
	Search     []string `json:"search,omitempty" yaml:"search,omitempty"`
	Nameserver []string `json:"nameserver,omitempty" yaml:"nameserver,omitempty"`
}

// Nameserver declaration
type Nameserver struct {
	Search    []string `json:"search,omitempty" yaml:"search,omitempty"`
	Addresses []string `json:"addresses,omitempty" yaml:"addresses,omitempty"`
}

// NetworkAdapter wrapper
type NetworkAdapter struct {
	DHCP4         bool                      `json:"dhcp4,omitempty" yaml:"dhcp4,omitempty"`
	NicName       *string                   `json:"set-name,omitempty" yaml:"set-name,omitempty"`
	Match         *map[string]string        `json:"match,omitempty" yaml:"match,omitempty"`
	Gateway4      *string                   `json:"gateway4,omitempty" yaml:"gateway4,omitempty"`
	Addresses     *[]any                    `json:"addresses,omitempty" yaml:"addresses,omitempty"`
	Nameservers   *Nameserver               `json:"nameservers,omitempty" yaml:"nameservers,omitempty"`
	DHCPOverrides CloudInit                 `json:"dhcp4-overrides,omitempty" yaml:"dhcp4-overrides,omitempty"`
	Routes        *[]v1alpha1.NetworkRoutes `json:"routes,omitempty" yaml:"routes,omitempty"`
}

// NetworkDeclare wrapper
type NetworkDeclare struct {
	Version   int                        `default:"2" json:"version,omitempty" yaml:"version,omitempty"`
	Ethernets map[string]*NetworkAdapter `json:"ethernets,omitempty" yaml:"ethernets,omitempty"`
}

// NetworkConfig wrapper
type NetworkConfig struct {
	InstanceID    string          `json:"instance-id,omitempty" yaml:"instance-id,omitempty"`
	LocalHostname string          `json:"local-hostname,omitempty" yaml:"local-hostname,omitempty"`
	Hostname      string          `json:"hostname,omitempty" yaml:"hostname,omitempty"`
	Network       *NetworkDeclare `json:"network,omitempty" yaml:"network,omitempty"`
}

func (g GuestInfos) IsEmpty() bool {
	return len(g) == 0
}

// Converts IP mask to 16 bit unsigned integer.
func addressToInteger(mask net.IP) uint32 {
	var i uint32

	buf := bytes.NewReader(mask)

	_ = binary.Read(buf, binary.BigEndian, &i)

	return i
}

// ToCIDR returns address in cidr format ww.xx.yy.zz/NN
func ToCIDR(address, netmask string) string {

	if len(netmask) == 0 {
		mask := net.ParseIP(address).DefaultMask()
		netmask = net.IPv4(mask[0], mask[1], mask[2], mask[3]).To4().String()
	}

	mask := net.ParseIP(netmask)
	netmask = strconv.FormatUint(uint64(addressToInteger(mask.To4())), 2)

	return fmt.Sprintf("%s/%d", address, strings.Count(netmask, "1"))
}

func EncodeCloudInit(name string, object any) (string, error) {
	var result string
	var out bytes.Buffer
	var err error

	fmt.Fprintln(&out, "#cloud-config")

	if object != nil {
		wr := yaml.NewEncoder(&out)

		wr.Encode(object)

		wr.Close()
	}

	if err == nil {
		var stdout bytes.Buffer
		var zw = gzip.NewWriter(&stdout)

		zw.Name = name
		zw.ModTime = time.Now()

		if glog.GetLevel() > glog.InfoLevel {
			fmt.Fprintf(os.Stderr, "name: %s\n%s", name, out.String())
		}

		if _, err = zw.Write(out.Bytes()); err == nil {
			if err = zw.Close(); err == nil {
				result = base64.StdEncoding.EncodeToString(stdout.Bytes())
			}
		}
	}

	return result, err
}

func EncodeObject(name string, object any) (string, error) {
	var result string
	out, err := yaml.Marshal(object)

	if err == nil {
		var stdout bytes.Buffer
		var zw = gzip.NewWriter(&stdout)

		zw.Name = name
		zw.ModTime = time.Now()

		if glog.GetLevel() > glog.InfoLevel {
			fmt.Fprintf(os.Stderr, "name: %s\n%s", name, string(out))
		}

		if _, err = zw.Write(out); err == nil {
			if err = zw.Close(); err == nil {
				result = base64.StdEncoding.EncodeToString(stdout.Bytes())
			}
		}
	}

	return result, err
}

func GeneratePublicKey(authKey string) (publicKey string, err error) {
	var priv []byte
	var key *rsa.PrivateKey
	var publicRsaKey ssh.PublicKey

	if priv, err = os.ReadFile(authKey); err != nil {
		glog.Errorf("unable to read: %s, reason: %v", authKey, err)
	} else {
		block, _ := pem.Decode([]byte(priv))

		if block == nil || block.Type != "RSA PRIVATE KEY" {
			glog.Errorf("failed to decode PEM block containing public key")
		} else if key, err = x509.ParsePKCS1PrivateKey(block.Bytes); err != nil {
			glog.Errorf("unable to parse private key: %s, reason: %v", authKey, err)
		} else if publicRsaKey, err = ssh.NewPublicKey(&key.PublicKey); err != nil {
			glog.Errorf("unable to generate public key: %s, reason: %v", authKey, err)
		} else {
			publicKey = string(ssh.MarshalAuthorizedKey(publicRsaKey))
		}
	}

	return
}

func (input *CloudInitInput) BuildVendorData() CloudInit {
	if input.UserName != "" && input.AuthKey != "" {
		if pubKey, err := GeneratePublicKey(input.AuthKey); err == nil {
			return CloudInit{
				"package_update":  input.AllowUpgrade,
				"package_upgrade": input.AllowUpgrade,
				"timezone":        input.TimeZone,
				"users": []string{
					"default",
				},
				"ssh_authorized_keys": []string{
					pubKey,
				},
				"system_info": CloudInit{
					"default_user": map[string]string{
						"name": input.UserName,
					},
				},
			}
		}
	}

	return CloudInit{
		"package_update":  input.AllowUpgrade,
		"package_upgrade": input.AllowUpgrade,
		"timezone":        input.TimeZone,
	}
}

func (input *CloudInitInput) BuildUserData(netplan string) (vendorData CloudInit, err error) {
	if netplan == "" {
		netplan = "51-custom.yaml"
	}

	vendorData = CloudInit{
		"package_update":  input.AllowUpgrade,
		"package_upgrade": input.AllowUpgrade,
		"timezone":        input.TimeZone,
	}

	for k, v := range input.CloudInit {
		vendorData[k] = v
	}

	vendorData.AddRunCommand("netplan apply")
	err = vendorData.AddObjectToWriteFile(CloudInit{"network": input.Network}, fmt.Sprintf("/etc/netplan/%s", netplan), "root:root", 0644)

	return
}

func (input *CloudInitInput) BuildGuestInfos() (GuestInfos, error) {
	var metadata, userdata, vendordata string
	var err error
	var guestInfos GuestInfos
	var fqdn string

	if len(input.DomainName) > 0 {
		fqdn = fmt.Sprintf("%s.%s", input.InstanceName, input.DomainName)
	}

	netconfig := &NetworkConfig{
		InstanceID:    input.InstanceID,
		LocalHostname: input.InstanceName,
		Hostname:      fqdn,
		Network:       input.Network,
	}

	if metadata, err = EncodeObject("metadata", netconfig); err != nil {
		return nil, fmt.Errorf(constantes.ErrUnableToEncodeGuestInfo, "metadata", err)
	}

	if userdata, err = EncodeCloudInit("userdata", input.CloudInit); err != nil {
		return nil, fmt.Errorf(constantes.ErrUnableToEncodeGuestInfo, "userdata", err)
	}

	if vendordata, err = EncodeCloudInit("vendordata", input.BuildVendorData()); err != nil {
		return nil, fmt.Errorf(constantes.ErrUnableToEncodeGuestInfo, "vendordata", err)
	}

	guestInfos = GuestInfos{
		"metadata":            metadata,
		"metadata.encoding":   GzipBase64,
		"userdata":            userdata,
		"userdata.encoding":   GzipBase64,
		"vendordata":          vendordata,
		"vendordata.encoding": GzipBase64,
	}

	return guestInfos, nil
}

func (c CloudInit) Clone() (copy CloudInit, err error) {
	err = constantes.Copy(&copy, c)

	return
}

func (c CloudInit) AddFileToWriteFile(sourceFile, dstDir, owner string) error {
	if content, err := os.ReadFile(sourceFile); err != nil {
		return err
	} else if info, err := os.Stat(sourceFile); err != nil {
		return err
	} else {
		c.AddToWriteFile(content, path.Join(dstDir, path.Base(sourceFile)), owner, uint(info.Mode()))
	}

	return nil
}

func (c CloudInit) AddDirectoryToWriteFile(srcDir, dstDir, owner string) error {
	if files, err := os.ReadDir(srcDir); err != nil {
		return err
	} else {
		for _, file := range files {
			if file.IsDir() {
				err = c.AddDirectoryToWriteFile(path.Join(srcDir, file.Name()), path.Join(dstDir, file.Name()), owner)
			} else {
				err = c.AddFileToWriteFile(path.Join(srcDir, file.Name()), dstDir, owner)
			}

			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (c CloudInit) AddObjectToWriteFile(object any, destination, owner string, permissions uint) error {
	if out, err := yaml.Marshal(object); err != nil {
		return err
	} else {
		c.AddToWriteFile(out, destination, owner, permissions)
	}

	return nil
}
func (c CloudInit) AddToWriteFile(content []byte, destination, owner string, permissions uint) {
	var oarr []any

	fileEntry := map[string]any{
		"encoding":    "b64",
		"owner":       owner,
		"path":        destination,
		"permissions": permissions,
		"content":     base64.StdEncoding.EncodeToString(content),
	}

	if write_files, found := c["write_files"]; found {
		oarr = write_files.([]any)
		oarr = append(oarr, fileEntry)
	} else {
		oarr = []any{fileEntry}
	}

	c["write_files"] = oarr
}

func (c CloudInit) AddTextToWriteFile(text string, destination, owner string, permissions uint) {
	c.AddToWriteFile([]byte(text), destination, owner, permissions)
}

func (c CloudInit) AddRunCommand(command ...string) {
	var oarr []any

	if runcmd, found := c["runcmd"]; found {
		v := runcmd.([]any)
		oarr = make([]any, 0, len(command)+len(v))
		oarr = append(oarr, v...)
	} else {
		oarr = make([]any, 0, len(command))
	}

	for _, cmd := range command {
		oarr = append(oarr, cmd)
	}

	c["runcmd"] = oarr
}
