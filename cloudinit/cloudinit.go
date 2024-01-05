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
	"strconv"
	"strings"
	"time"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/constantes"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/pkg/apis/nodemanager/v1alpha1"
	glog "github.com/sirupsen/logrus"
	"github.com/vmware/govmomi/vim25/types"
	"golang.org/x/crypto/ssh"
	"gopkg.in/yaml.v2"
)

const GzipBase64 = "gzip+base64"

// GuestInfos the guest infos
// Must not start with `guestinfo.`
type GuestInfos map[string]string

type CloudInitInput struct {
	InstanceName string
	InstanceID   string //string(uuid.NewUUID())
	DomainName   string
	UserName     string
	AuthKey      string
	TimeZone     string
	Network      *NetworkDeclare
	AllowUpgrade bool
	CloudInit    interface{}
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
	Addresses     *[]string                 `json:"addresses,omitempty" yaml:"addresses,omitempty"`
	Nameservers   *Nameserver               `json:"nameservers,omitempty" yaml:"nameservers,omitempty"`
	DHCPOverrides *map[string]interface{}   `json:"dhcp4-overrides,omitempty" yaml:"dhcp4-overrides,omitempty"`
	Routes        *[]v1alpha1.NetworkRoutes `json:"routes,omitempty" yaml:"routes,omitempty"`
}

// NetworkDeclare wrapper
type NetworkDeclare struct {
	Version   int                        `json:"version,omitempty" yaml:"version,omitempty"`
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

func (g GuestInfos) ToExtraConfig() []types.BaseOptionValue {
	extraConfig := make([]types.BaseOptionValue, len(g))

	for k, v := range g {
		extraConfig = append(extraConfig,
			&types.OptionValue{
				Key:   fmt.Sprintf("guestinfo.%s", k),
				Value: v,
			})
	}

	return extraConfig
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

func EncodeCloudInit(name string, object interface{}) (string, error) {
	var result string
	var out bytes.Buffer
	var err error

	fmt.Fprintln(&out, "#cloud-config")

	wr := yaml.NewEncoder(&out)
	err = wr.Encode(object)

	wr.Close()

	if err == nil {
		var stdout bytes.Buffer
		var zw = gzip.NewWriter(&stdout)

		zw.Name = name
		zw.ModTime = time.Now()

		if _, err = zw.Write(out.Bytes()); err == nil {
			if err = zw.Close(); err == nil {
				result = base64.StdEncoding.EncodeToString(stdout.Bytes())
			}
		}
	}

	return result, err
}

func EncodeObject(name string, object interface{}) (string, error) {
	var result string
	out, err := yaml.Marshal(object)

	if err == nil {
		var stdout bytes.Buffer
		var zw = gzip.NewWriter(&stdout)

		zw.Name = name
		zw.ModTime = time.Now()

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
		glog.Errorf("unable to read:%s, reason: %v", authKey, err)
	} else {
		block, _ := pem.Decode([]byte(priv))

		if block == nil || block.Type != "RSA PRIVATE KEY" {
			glog.Errorf("failed to decode PEM block containing public key")
		} else if key, err = x509.ParsePKCS1PrivateKey(block.Bytes); err != nil {
			glog.Errorf("unable to parse private key:%s, reason: %v", authKey, err)
		} else if publicRsaKey, err = ssh.NewPublicKey(&key.PublicKey); err != nil {
			glog.Errorf("unable to generate public key:%s, reason: %v", authKey, err)
		} else {
			publicKey = string(ssh.MarshalAuthorizedKey(publicRsaKey))
		}
	}

	return
}

func BuildVendorData(userName, authKey, tz string, allowUpgrade bool) interface{} {
	if userName != "" && authKey != "" {
		if pubKey, err := GeneratePublicKey(authKey); err == nil {
			return map[string]interface{}{
				"package_update":  allowUpgrade,
				"package_upgrade": allowUpgrade,
				"timezone":        tz,
				"users": []string{
					"default",
				},
				"ssh_authorized_keys": []string{
					pubKey,
				},
				"system_info": map[string]interface{}{
					"default_user": map[string]string{
						"name": userName,
					},
				},
			}
		}
	}
	return map[string]interface{}{
		"package_update":  allowUpgrade,
		"package_upgrade": allowUpgrade,
		"timezone":        tz,
	}
}

func BuildCloudInit(input *CloudInitInput) (GuestInfos, error) {
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
		err = fmt.Errorf(constantes.ErrUnableToEncodeGuestInfo, "metadata", err)
	}

	if err == nil {
		if input.CloudInit != nil {
			if userdata, err = EncodeCloudInit("userdata", input.CloudInit); err != nil {
				return nil, fmt.Errorf(constantes.ErrUnableToEncodeGuestInfo, "userdata", err)
			}
		} else if userdata, err = EncodeCloudInit("userdata", map[string]string{}); err != nil {
			return nil, fmt.Errorf(constantes.ErrUnableToEncodeGuestInfo, "userdata", err)
		}

		if vendordata, err = EncodeCloudInit("vendordata", BuildVendorData(input.UserName, input.AuthKey, input.TimeZone, input.AllowUpgrade)); err != nil {
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
	}

	return guestInfos, nil
}
