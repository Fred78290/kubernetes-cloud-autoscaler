/*
Copyright 2020 The cert-manager Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package rfc2136 implements a DNS provider for solving the DNS-01 challenge
// using the rfc2136 dynamic update.
// This code was adapted from lego:
// 	  https://github.com/xenolf/lego

package rfc2136

import (
	"encoding/base64"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/miekg/dns"
	glog "github.com/sirupsen/logrus"
	"github.com/stianwa/rndc"
)

const defaultRFC2136Port = "53"

const (
	addRecord    = 1
	deleteRecord = 2
)

// RFC2136Provider is an implementation of the acme.ChallengeProvider interface that
// uses dynamic DNS updates (RFC 2136) to create TXT records on a nameserver.
type RFC2136Provider struct {
	nameserver string
	tsigKey    *rndc.Key

	tsigKeyName string
	tsigSecret  string
}

func loadRndcKeyFile(name string) (string, *rndc.Key, error) {
	m, err := rndc.LoadKeyMap(name)
	if err != nil {
		return "", nil, err
	}

	for k, v := range m {
		return k, v, nil
	}

	return "", nil, fmt.Errorf("no keys in file")
}

// NewDNSRFC2136ProviderCredentials uses the supplied credentials to return a
// DNSProvider instance configured for rfc2136 dynamic update. To disable TSIG
// authentication, leave the TSIG parameters as empty strings.
// nameserver must be a network address in the form "IP" or "IP:port".
func NewDNSRFC2136ProviderCredentials(nameserver, rndcKeyFile string) (d *RFC2136Provider, err error) {
	glog.Debug("Creating RFC2136 Provider")

	d = &RFC2136Provider{}

	if d.nameserver, err = validNameserver(nameserver); err != nil {
		return
	}

	if d.tsigKeyName, d.tsigKey, err = loadRndcKeyFile(rndcKeyFile); err != nil {
		return
	}

	d.tsigSecret = base64.StdEncoding.EncodeToString(d.tsigKey.Secret)

	if glog.GetLevel() >= glog.DebugLevel {
		keyLen := len(d.tsigSecret)
		mask := make([]rune, keyLen/2)

		for i := range mask {
			mask[i] = '*'
		}

		masked := d.tsigSecret[0:keyLen/4] + string(mask) + d.tsigSecret[keyLen/4*3:keyLen]

		glog.Debugf("RFC2136Provider nameserver:       %s\n", d.nameserver)
		glog.Debugf("                tsigAlgorithm:    %s\n", d.tsigKey.Algorithm)
		glog.Debugf("                tsigKeyName:      %s\n", d.tsigKeyName)
		glog.Debugf("                tsigSecret:       %s\n", masked)
	}

	return
}

// AddRecord creates a record using the specified parameters
func (r *RFC2136Provider) AddRecord(fqdn, zone, value string) error {
	return r.changeRecord(addRecord, dns.Fqdn(fqdn), dns.Fqdn(zone), value, 60)
}

// CleanUp removes the TXT record matching the specified parameters
func (r *RFC2136Provider) RemoveRecord(fqdn, zone, value string) error {
	return r.changeRecord(deleteRecord, dns.Fqdn(fqdn), dns.Fqdn(zone), value, 60)
}

func (r *RFC2136Provider) changeRecord(action int, fqdn, zone, value string, ttl int) error {
	var m dns.Msg
	var c dns.Client

	// Create RR
	rrs := []dns.RR{
		&dns.A{
			A: net.ParseIP(value).To4(),
			Hdr: dns.RR_Header{
				Name:   fqdn,
				Rrtype: dns.TypeA,
				Class:  dns.ClassINET,
				Ttl:    uint32(ttl),
			},
		},
	}

	// Create dynamic update packet
	m.SetUpdate(zone)

	switch action {
	case addRecord:
		m.Insert(rrs)
	case deleteRecord:
		m.Remove(rrs)
	default:
		return fmt.Errorf("unsupported action: %d", action)
	}

	// Setup client
	c.TsigProvider = tsigHMACProvider(r.tsigSecret)

	// TSIG authentication / msg signing
	if len(r.tsigKeyName) > 0 && len(r.tsigSecret) > 0 {
		m.SetTsig(dns.Fqdn(r.tsigKeyName), dns.Fqdn(r.tsigKey.Algorithm), 300, time.Now().Unix())

		c.TsigSecret = map[string]string{
			dns.Fqdn(r.tsigKeyName): r.tsigSecret,
		}
	}

	// Send the query
	if reply, _, err := c.Exchange(&m, r.nameserver); err != nil {
		return fmt.Errorf("DNS update failed: %v", err)
	} else if reply != nil && reply.Rcode != dns.RcodeSuccess {
		return fmt.Errorf("DNS update failed. Server replied: %s", dns.RcodeToString[reply.Rcode])
	}

	return nil
}

// Nameserver returns the nameserver configured for this provider when it was created
func (r *RFC2136Provider) Nameserver() string {
	return r.nameserver
}

// TSIGAlgorithm returns the TSIG algorithm configured for this provider when it was created
func (r *RFC2136Provider) TSIGAlgorithm() string {
	return r.tsigKey.Algorithm
}

// TSIGAlgorithm returns the TSIG algorithm configured for this provider when it was created
func (r *RFC2136Provider) TSIGSecret() string {
	return r.tsigSecret
}

// validNameserver validates the given nameserver for the RFC2136 provider, returning the sanitized nameserver - if valid - in the form "<host>:<port>".
func validNameserver(nameserver string) (host string, err error) {
	port := ""
	nameserver = strings.TrimSpace(nameserver)

	if nameserver == "" {
		return "", fmt.Errorf("RFC2136 nameserver missing")
	}

	// SplitHostPort Behavior
	// nameserver          host                port    err
	// 8.8.8.8             ""                  ""      missing port in address
	// 8.8.8.8:            "8.8.8.8"           ""      <nil>
	// 8.8.8.8.8:53        "8.8.8.8"           53      <nil>
	// [2001:db8::1]       ""                  ""      missing port in address
	// [2001:db8::1]:      "2001:db8::1"       ""      <nil>
	// [2001:db8::1]:53    "2001:db8::1"       53      <nil>
	// nameserver.com      ""                  ""      missing port in address
	// nameserver.com:     "nameserver.com"    ""      <nil>
	// nameserver.com:53   "nameserver.com"    53      <nil>
	// :53                 ""                  53      <nil>
	if host, port, err = net.SplitHostPort(nameserver); err != nil {
		if strings.Contains(err.Error(), "missing port") {
			// net.JoinHostPort expect IPv6 address to be unenclosed
			host = strings.Trim(nameserver, "[]")
		} else {
			return "", fmt.Errorf("RFC2136 nameserver is invalid: %s", err.Error())
		}
	}

	if host == "" {
		return "", fmt.Errorf("RFC2136 nameserver has no host defined, %v", nameserver)
	}

	if port == "" {
		port = defaultRFC2136Port
	}

	return net.JoinHostPort(host, port), nil
}
