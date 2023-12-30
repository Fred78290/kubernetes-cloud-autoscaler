package desktop

import (
	"bytes"
	"compress/gzip"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"os"
	"time"

	glog "github.com/sirupsen/logrus"
	"golang.org/x/crypto/ssh"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/constantes"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/providers/desktop/api"
	"gopkg.in/yaml.v2"
	"k8s.io/apimachinery/pkg/util/uuid"
)

// VirtualMachine virtual machine wrapper
type VirtualMachine struct {
	Name   string
	Uuid   string
	Vmx    string
	Vcpus  int32
	Memory int64
}

// GuestInfos the guest infos
// Must not start with `guestinfo.`
type GuestInfos map[string]string

func encodeCloudInit(name string, object interface{}) (string, error) {
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

func encodeObject(name string, object interface{}) (string, error) {
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

func generatePublicKey(authKey string) (publicKey string, err error) {
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

func buildVendorData(userName, authKey, tz string, allowUpgrade bool) interface{} {
	if userName != "" && authKey != "" {
		if pubKey, err := generatePublicKey(authKey); err == nil {
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

// BuildCloudInit build map for guestinfo
func BuildCloudInit(hostName, userName, authKey, tz string, cloudInit interface{}, network *Network, nodeIndex int, allowUpgrade bool) (GuestInfos, error) {
	var metadata, userdata, vendordata string
	var err error
	var guestInfos GuestInfos
	var fqdn string

	if len(network.Domain) > 0 {
		fqdn = fmt.Sprintf("%s.%s", hostName, network.Domain)
	}

	netconfig := &NetworkConfig{
		InstanceID:    string(uuid.NewUUID()),
		LocalHostname: hostName,
		Hostname:      fqdn,
	}

	if network != nil && len(network.Interfaces) > 0 {
		netconfig.Network = network.GetCloudInitNetwork(nodeIndex)
	}

	if metadata, err = encodeObject("metadata", netconfig); err != nil {
		err = fmt.Errorf(constantes.ErrUnableToEncodeGuestInfo, "metadata", err)
	}

	if err == nil {
		if cloudInit != nil {
			if userdata, err = encodeCloudInit("userdata", cloudInit); err != nil {
				return nil, fmt.Errorf(constantes.ErrUnableToEncodeGuestInfo, "userdata", err)
			}
		} else if userdata, err = encodeCloudInit("userdata", map[string]string{}); err != nil {
			return nil, fmt.Errorf(constantes.ErrUnableToEncodeGuestInfo, "userdata", err)
		}

		if vendordata, err = encodeCloudInit("vendordata", buildVendorData(userName, authKey, tz, allowUpgrade)); err != nil {
			return nil, fmt.Errorf(constantes.ErrUnableToEncodeGuestInfo, "vendordata", err)
		}

		guestInfos = GuestInfos{
			"metadata":            metadata,
			"metadata.encoding":   "gzip+base64",
			"userdata":            userdata,
			"userdata.encoding":   "gzip+base64",
			"vendordata":          vendordata,
			"vendordata.encoding": "gzip+base64",
		}
	}

	return guestInfos, nil
}

func BuildNetworkInterface(interfaces []*NetworkInterface, nodeIndex int) []*api.NetworkInterface {
	result := make([]*api.NetworkInterface, 0, len(interfaces))

	for _, inf := range interfaces {
		result = append(result, &api.NetworkInterface{
			Macaddress:  inf.GetMacAddress(nodeIndex),
			Vnet:        inf.VNet,
			Type:        inf.ConnectionType,
			Device:      inf.VirtualDev,
			BsdName:     inf.BsdName,
			DisplayName: inf.DisplayName,
		})
	}
	return result
}
